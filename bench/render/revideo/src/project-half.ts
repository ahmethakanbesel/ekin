import { makeProject } from "@revideo/core";
import "./fonts.css";
import bench from "./scenes/bench?scene";

export default makeProject({
  scenes: [bench],
  settings: {
    shared: { size: { x: 1080, y: 1920 }, background: "#000" },
    rendering: { fps: 30, resolutionScale: 0.5, exporter: { name: "@revideo/core/ffmpeg", options: { format: "mp4" } } },
  },
});
