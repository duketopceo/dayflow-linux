import React from 'react';
import {Composition} from 'remotion';
import {Fonts} from './core/Fonts';
import {
  DayflowLaunchV2,
  DayflowLaunchV2Square,
  DAYFLOW_V2_FRAMES,
} from './videos/dayflow/launch/v2';
import {DayflowLoopV2} from './videos/dayflow/launch/v2/LoopV2';

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
        id="DayflowLaunchV2"
        component={() => (
          <WithFonts>
            <DayflowLaunchV2 />
          </WithFonts>
        )}
        durationInFrames={DAYFLOW_V2_FRAMES}
        fps={60}
        width={1920}
        height={1080}
      />
      <Composition
        id="DayflowLaunchV2Square"
        component={() => (
          <WithFonts>
            <DayflowLaunchV2Square />
          </WithFonts>
        )}
        durationInFrames={DAYFLOW_V2_FRAMES}
        fps={60}
        width={1080}
        height={1080}
      />
      <Composition
        id="DayflowLoopV2"
        component={() => (
          <WithFonts>
            <DayflowLoopV2 />
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
