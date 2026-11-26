package workspace

import (
	"embed"
	"strings"
)

// WorkflowGuide is shared by skill loading, capability activation and draft roles.
//
//go:embed builtin/workflows/SKILL.md
var WorkflowGuide string

//go:embed builtin/visual-investigation/SKILL.md
var visualGuide string

//go:embed builtin/research-sources/SKILL.md
var researchGuide string

//go:embed builtin/research-sources/references/*.md
var researchReferences embed.FS

func workflowSkill() Skill {
	return bundledSkill("workflows", WorkflowGuide)
}

func bundledSkill(id, guide string) Skill {
	meta, err := readSkillMetadataLimit(strings.NewReader(guide), defaultSkillDescriptionLimit)
	if err != nil {
		panic("invalid bundled skill " + id + ": " + err.Error())
	}
	return Skill{ID: id, Name: meta.Name, Description: meta.Description, Path: "builtin:" + id + "/SKILL.md", BaseDir: "builtin:" + id, Source: "builtin"}
}
