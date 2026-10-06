import React from 'react';
import {Composition} from 'remotion';
import {Fonts} from './core/Fonts';
import {
  DayflowLaunch,
  DayflowLaunchSquare,
  DAYFLOW_FRAMES,
} from './videos/dayflow/launch';
import {DayflowLoop} from './videos/dayflow/launch/Loop';

const WithFonts: React.FC<{children: React.ReactNode}> = ({children}) => (
  <>
    <Fonts />
    {children}
  </>
);

export const Root: React.FC = () => {
  return (
    <>
      <Composition
        id="DayflowLaunch"
        component={() => (
          <WithFonts>
            <DayflowLaunch />
          </WithFonts>
        )}
        durationInFrames={DAYFLOW_FRAMES}
        fps={60}
        width={1920}
        height={1080}
      />
      <Composition
        id="DayflowLaunchSquare"
        component={() => (
          <WithFonts>
            <DayflowLaunchSquare />
          </WithFonts>
        )}
        durationInFrames={DAYFLOW_FRAMES}
        fps={60}
        width={1080}
        height={1080}
      />
      <Composition
        id="DayflowLoop"
        component={() => (
          <WithFonts>
            <DayflowLoop />
          </WithFonts>
        )}
        durationInFrames={300}
        fps={60}
        width={1920}
        height={1080}
      />
    </>
  );
};
