#ifndef DAYFLOW_PWBRIDGE_H
#define DAYFLOW_PWBRIDGE_H

#include <stdint.h>

// Opaque session: main loop + core connected on the portal-provided fd.
typedef struct pw_bridge pw_bridge;

// Lifecycle: init once, connect on the portal fd, run() blocks until quit().
int  pw_bridge_init(void);
pw_bridge *pw_bridge_connect(int fd);
int  pw_bridge_add_stream(pw_bridge *b, uint32_t node_id);
void pw_bridge_run(pw_bridge *b);      // blocks; drives all stream callbacks
void pw_bridge_stop(pw_bridge *b);     // safe from another thread
void pw_bridge_free(pw_bridge *b);

#endif
