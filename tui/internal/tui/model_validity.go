package tui

// Real-availability probing for a preset's exact (provider, model,
// credential) tuple used to gate Save in the preset editor and the
// first-run wizard. That save-time gate has been removed: saving a
// preset now performs only local structural validation
// (preset.Preset.Validate), never a live provider/model network call.
// See tui/internal/preset/skills/lingtai-preset-skill/reference/operations/availability-save-gate/SKILL.md.
//
// Live availability diagnosis is owned by /doctor (doctor.go's probeLLM)
// and by actual runtime execution — not by this file.
