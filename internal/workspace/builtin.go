package workspace

import (
	_ "embed"
	"strings"
)

// WorkflowGuide is shared by skill loading, capability activation and draft roles.
//
//go:embed builtin/workflows/SKILL.md
var WorkflowGuide string

func workflowSkill() Skill {
	meta, err := readSkillMetadataLimit(strings.NewReader(WorkflowGuide), defaultSkillDescriptionLimit)
	if err != nil {
		panic("invalid bundled workflow guide: " + err.Error())
	}
	return Skill{ID: "workflows", Name: meta.Name, Description: meta.Description, Path: "builtin:workflows/SKILL.md", BaseDir: "builtin:workflows", Source: "builtin"}
}
