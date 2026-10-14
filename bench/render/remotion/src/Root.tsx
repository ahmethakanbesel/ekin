import { Composition } from "remotion";
import spec from "../../shared/spec.json";
import { Bench } from "./Bench";

export const Root = () => (
  <Composition
    id="Bench"
    component={Bench}
    durationInFrames={spec.duration * spec.fps}
    fps={spec.fps}
    width={spec.width}
    height={spec.height}
  />
);
