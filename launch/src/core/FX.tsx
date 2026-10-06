import React from 'react';
import {useCurrentFrame, interpolate, spring, useVideoConfig} from 'remotion';
import {color} from '../brands/dayflow/tokens';

/**
 * Atmosphere + impact primitives — the layer stack that keeps scenes from
 * reading as flat divs: ambient glow, film grain, shockwave ring, light sweep.
 * All motion from useCurrentFrame; nothing drifts without a cause.
 */

/** Slow ambient glow — background layer. */
export const GlowOrb: React.FC<{
  x: number;
  y: number;
  size: number;
  tint?: string;
  opacity?: number;
  drift?: number;
}> = ({x, y, size, tint = color.accent, opacity = 0.16, drift = 18}) => {
  const frame = useCurrentFrame();
  const dx = Math.sin(frame / 140) * drift;
  const dy = Math.cos(frame / 170) * drift * 0.6;
  return (
    <div
      style={{
        position: 'absolute',
        left: x - size / 2 + dx,
        top: y - size / 2 + dy,
        width: size,
        height: size,
        borderRadius: '50%',
        background: `radial-gradient(circle, ${tint}${Math.round(
          opacity * 255
        )
          .toString(16)
          .padStart(2, '0')} 0%, transparent 70%)`,
        pointerEvents: 'none',
      }}
    />
  );
};

/** Film grain — topmost layer, ~4% opacity. Cheap SVG turbulence. */
export const Grain: React.FC<{opacity?: number}> = ({opacity = 0.05}) => (
  <svg
    style={{
      position: 'absolute',
      inset: 0,
      width: '100%',
      height: '100%',
      opacity,
      mixBlendMode: 'overlay',
      pointerEvents: 'none',
    }}
  >
    <filter id="df-grain">
      <feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="2" />
    </filter>
    <rect width="100%" height="100%" filter="url(#df-grain)" />
  </svg>
);

/** Shockwave ring — fires at `at`, expands + dies. Impact moments only. */
export const Ring: React.FC<{
  at: number;
  x: number;
  y: number;
  tint?: string;
  maxR?: number;
}> = ({at, x, y, tint = color.accent, maxR = 320}) => {
  const frame = useCurrentFrame();
  const t = frame - at;
  if (t < 0 || t > 30) return null;
  const r = interpolate(t, [0, 30], [8, maxR], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });
  const o = interpolate(t, [0, 30], [0.7, 0], {
    extrapolateRight: 'clamp',
  });
  const w = interpolate(t, [0, 30], [4, 1], {extrapolateRight: 'clamp'});
  return (
    <div
      style={{
        position: 'absolute',
        left: x - r,
        top: y - r,
        width: r * 2,
        height: r * 2,
        borderRadius: '50%',
        border: `${w}px solid ${tint}`,
        opacity: o,
        pointerEvents: 'none',
      }}
    />
  );
};

/** Light sweep — diagonal shine passing over an element once. */
export const Sweep: React.FC<{
  at: number;
  duration?: number;
}> = ({at, duration = 26}) => {
  const frame = useCurrentFrame();
  const t = frame - at;
  if (t < 0 || t > duration) return null;
  const p = interpolate(t, [0, duration], [-60, 160]);
  const o = interpolate(t, [0, 8, duration - 6, duration], [0, 1, 1, 0]);
  return (
    <div
      style={{
        position: 'absolute',
        inset: 0,
        overflow: 'hidden',
        pointerEvents: 'none',
        borderRadius: 'inherit',
      }}
    >
      <div
        style={{
          position: 'absolute',
          top: '-20%',
          bottom: '-20%',
          left: `${p}%`,
          width: '18%',
          transform: 'skewX(-18deg)',
          background:
            'linear-gradient(90deg, transparent, rgba(155,212,189,0.28), transparent)',
          opacity: o,
        }}
      />
    </div>
  );
};

/** Spring value helper — returns 0→1 with physical overshoot. */
export const useSlam = (
  delay: number,
  cfg: {damping?: number; stiffness?: number; mass?: number} = {}
) => {
  const frame = useCurrentFrame();
  const {fps} = useVideoConfig();
  if (frame < delay) return 0;
  return spring({
    frame: frame - delay,
    fps,
    config: {damping: 13, stiffness: 220, mass: 0.9, ...cfg},
  });
};
