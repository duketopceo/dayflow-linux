// PipeWire bridge: minimal cgo glue between the Go frame assembler and
// libpipewire. The portal fd is consumed via pw_context_connect_fd — never
// the ambient socket — which is what keeps strict snap confinement viable.
//
// Frame delivery: process callback dequeues each buffer, hands the mapped
// MemFd span to Go (copied there — the buffer is requeued immediately),
// and reports stream state changes for supervisor accounting.
#include "pwbridge.h"

#include <errno.h>
#include <pipewire/pipewire.h>
#include <spa/param/video/format-utils.h>
#include <spa/param/props.h>
#include <string.h>

// Implemented in pw.go via //export.
extern void goStreamFrame(int node, int w, int h, int stride, int format,
                          void *data, int len);
extern void goStreamState(int node, int state, const char *msg);
extern void goStreamFormat(int node, int w, int h);

#define PW_MAX_STREAMS 64

struct stream_node {
	struct pw_stream *stream;
	struct spa_hook listener;
	uint32_t node_id;
	struct spa_video_info_raw info;
};

struct pw_bridge {
	struct pw_main_loop *loop;
	struct pw_context *ctx;
	struct pw_core *core;
	struct stream_node *streams[PW_MAX_STREAMS];
	int nstreams;
};

int pw_bridge_init(void) {
	pw_init(NULL, NULL);
	return 0;
}

pw_bridge *pw_bridge_connect(int fd) {
	pw_bridge *b = calloc(1, sizeof(pw_bridge));
	if (!b) return NULL;
	b->loop = pw_main_loop_new(NULL);
	if (!b->loop) { free(b); return NULL; }
	b->ctx = pw_context_new(pw_main_loop_get_loop(b->loop), NULL, 0);
	if (!b->ctx) { pw_main_loop_destroy(b->loop); free(b); return NULL; }
	// fd ownership: connect_fd consumes the portal fd on success.
	b->core = pw_context_connect_fd(b->ctx, fd, NULL, 0);
	if (!b->core) {
		pw_context_destroy(b->ctx);
		pw_main_loop_destroy(b->loop);
		free(b);
		return NULL;
	}
	return b;
}

static void stream_process(void *userdata) {
	struct stream_node *sn = userdata;
	struct pw_buffer *pb = pw_stream_dequeue_buffer(sn->stream);
	if (!pb) return;
	struct spa_buffer *buf = pb->buffer;
	if (buf->n_datas > 0) {
		struct spa_data *sd = &buf->datas[0];
		// MAP_BUFFERS flag mapped the chunk for us — sd->data non-NULL.
		// Clamp offset+size to maxsize: the span comes from the producer
		// and an oversize chunk must never read past the mapped region.
		if (sd->data && sd->chunk->offset <= sd->maxsize) {
			uint32_t avail = sd->maxsize - sd->chunk->offset;
			int len = sd->chunk->size ? (int)sd->chunk->size : (int)avail;
			if (len > (int)avail) len = (int)avail;
			uint8_t *base = (uint8_t *)sd->data + sd->chunk->offset;
			goStreamFrame((int)sn->node_id,
			              (int)sn->info.size.width, (int)sn->info.size.height,
			              (int)sd->chunk->stride, (int)sn->info.format,
			              base, len);
		}
	}
	pw_stream_queue_buffer(sn->stream, pb);
}

static void stream_param_changed(void *userdata, uint32_t id,
                                 const struct spa_pod *param) {
	struct stream_node *sn = userdata;
	if (!param || id != SPA_PARAM_Format) return;
	if (spa_format_video_raw_parse(param, &sn->info) < 0) return;
	goStreamFormat((int)sn->node_id,
	               (int)sn->info.size.width, (int)sn->info.size.height);
	// Ask for MemFd data explicitly so sd->data is a mmap'd span.
	uint8_t buf[256];
	struct spa_pod_builder pb = SPA_POD_BUILDER_INIT(buf, sizeof(buf));
	const struct spa_pod *params[1];
	params[0] = spa_pod_builder_add_object(&pb,
	    SPA_TYPE_OBJECT_ParamBuffers, SPA_PARAM_Buffers,
	    SPA_PARAM_BUFFERS_dataType, SPA_POD_Int(1 << SPA_DATA_MemFd));
	pw_stream_update_params(sn->stream, params, 1);
}

static void stream_state_changed(void *userdata, enum pw_stream_state old,
                                 enum pw_stream_state state, const char *err) {
	struct stream_node *sn = userdata;
	goStreamState((int)sn->node_id, (int)state, err ? err : "");
}

static const struct pw_stream_events stream_events = {
	PW_VERSION_STREAM_EVENTS,
	.state_changed = stream_state_changed,
	.param_changed = stream_param_changed,
	.process = stream_process,
};

int pw_bridge_add_stream(pw_bridge *b, uint32_t node_id) {
	if (b->nstreams >= PW_MAX_STREAMS) return -1;
	struct stream_node *sn = calloc(1, sizeof(struct stream_node));
	if (!sn) return -1;
	sn->node_id = node_id;
	sn->info = (struct spa_video_info_raw) SPA_VIDEO_INFO_RAW_INIT();
	sn->info.format = SPA_VIDEO_FORMAT_BGRA;
	sn->info.size = SPA_RECTANGLE(0, 0); // let the producer choose native res

	struct pw_properties *props = pw_properties_new(
	    PW_KEY_MEDIA_TYPE, "Video",
	    PW_KEY_MEDIA_CATEGORY, "Capture",
	    PW_KEY_MEDIA_ROLE, "Screen",
	    PW_KEY_STREAM_CAPTURE_SINK, "true",
	    NULL);
	sn->stream = pw_stream_new(b->core, "dayflow-portal", props);
	if (!sn->stream) { free(sn); return -1; }
	pw_stream_add_listener(sn->stream, &sn->listener, &stream_events, sn);

	uint8_t buf[1024];
	struct spa_pod_builder pb = SPA_POD_BUILDER_INIT(buf, sizeof(buf));
	const struct spa_pod *params[1];
	params[0] = spa_format_video_raw_build(&pb, SPA_PARAM_EnumFormat, &sn->info);
	if (pw_stream_connect(sn->stream, PW_DIRECTION_INPUT, node_id,
	                      PW_STREAM_FLAG_AUTOCONNECT |
	                      PW_STREAM_FLAG_MAP_BUFFERS |
	                      PW_STREAM_FLAG_RT_PROCESS,
	                      params, 1) < 0) {
		pw_stream_destroy(sn->stream);
		free(sn);
		return -1;
	}
	b->streams[b->nstreams++] = sn;
	return 0;
}

void pw_bridge_run(pw_bridge *b) { pw_main_loop_run(b->loop); }
void pw_bridge_stop(pw_bridge *b) { pw_main_loop_quit(b->loop); }

void pw_bridge_free(pw_bridge *b) {
	if (!b) return;
	for (int i = 0; i < b->nstreams; i++) {
		if (b->streams[i]->stream) pw_stream_destroy(b->streams[i]->stream);
		free(b->streams[i]);
	}
	if (b->core) pw_core_disconnect(b->core);
	if (b->ctx) pw_context_destroy(b->ctx);
	if (b->loop) pw_main_loop_destroy(b->loop);
	free(b);
}
