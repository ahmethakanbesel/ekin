window.ekinMusic = (Tone, { duration }) => {
  const synth = new Tone.PolySynth(Tone.Synth).toDestination();
  synth.volume.value = -12;
  const chords = [["C4", "E4", "G4"], ["A3", "C4", "E4"]];
  for (let i = 0; i * 1 < duration; i++) synth.triggerAttackRelease(chords[i % 2], 0.9, i);
};
