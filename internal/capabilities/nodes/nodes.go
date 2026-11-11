// Package nodes adapts native capabilities to workflow node definitions.
// Each family exposes its own definitions and retains its authorization contract.
package nodes

import (
	"github.com/keshon/gektor/internal/capabilities/documents"
	documentscontract "github.com/keshon/gektor/internal/capabilities/documents/contract"
	"github.com/keshon/gektor/internal/capabilities/files"
	filescontract "github.com/keshon/gektor/internal/capabilities/files/contract"
	"github.com/keshon/gektor/internal/capabilities/images"
	imagescontract "github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/capabilities/media"
	mediacontract "github.com/keshon/gektor/internal/capabilities/media/contract"
	"github.com/keshon/gektor/internal/capabilities/social"
	socialcontract "github.com/keshon/gektor/internal/capabilities/social/contract"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func DocumentDefinitions(ws *workspace.Workspace) []workflow.Node {
	var result []workflow.Node

	for _, kind := range documentscontract.Names() {
		op := documentscontract.Operation{Service: documents.Service{WS: ws}, Kind: kind}
		result = append(result, workflow.Node{
			Name:             kind,
			Version:          1,
			Description:      op.Description(),
			InputSchema:      op.Schema(),
			OutputSchema:     op.OutputSchema(),
			ReadOnly:         op.ReadOnly(),
			RetrySafe:        op.RetrySafe(),
			Validate:         op.Validate,
			Resources:        op.Resources,
			Execute:          op.Run,
			ValidateTemplate: op.ValidateTemplate,
		})
	}

	return result
}

func FileDefinitions(ws *workspace.Workspace, authorize filescontract.OutputAuthorizer) []workflow.Node {
	var result []workflow.Node

	for _, name := range filescontract.Names() {
		op := filescontract.Operation{
			Service:         files.Service{WS: ws},
			Kind:            name,
			AuthorizeOutput: authorize,
		}
		result = append(result, workflow.Node{
			Name:             op.Name(),
			Version:          1,
			Description:      op.Description(),
			InputSchema:      op.Schema(),
			OutputSchema:     op.OutputSchema(),
			ReadOnly:         op.ReadOnly(),
			RetrySafe:        op.RetrySafe(),
			Resources:        op.Resources,
			Validate:         op.Validate,
			Execute:          op.Run,
			ValidateTemplate: op.ValidateTemplate,
		})
	}

	return result
}

func ImageDefinitions(ws *workspace.Workspace, output imagescontract.OutputAuthorizer) []workflow.Node {
	var nodes []workflow.Node

	for _, name := range imagescontract.Names() {
		op := imagescontract.Operation{
			Service:         images.Service{WS: ws},
			Kind:            name,
			AuthorizeOutput: output,
		}
		nodes = append(nodes, workflow.Node{
			Name:             op.Name(),
			Version:          1,
			Description:      op.Description(),
			InputSchema:      op.Schema(),
			OutputSchema:     op.OutputSchema(),
			ReadOnly:         op.ReadOnly(),
			RetrySafe:        op.RetrySafe(),
			Resources:        op.Resources,
			Execute:          op.Run,
			Validate:         op.Validate,
			ValidateTemplate: op.ValidateTemplate,
		})
	}

	return nodes
}

func MediaDefinitions(svc *media.Service) []workflow.Node {
	result := []workflow.Node{}

	for _, name := range mediacontract.Names() {
		op := mediacontract.Operation{Service: svc, Kind: name}
		result = append(result, workflow.Node{
			Name:             name,
			Version:          1,
			Description:      op.Description(),
			InputSchema:      op.Schema(),
			OutputSchema:     op.OutputSchema(),
			ReadOnly:         op.ReadOnly(),
			RetrySafe:        op.RetrySafe(),
			Resources:        op.Resources,
			Validate:         op.Validate,
			Execute:          op.Run,
			ValidateTemplate: op.ValidateTemplate,
		})
	}

	return result
}

func SocialDefinitions(s social.Service) []workflow.Node {
	op := socialcontract.Operation{Service: s}
	return []workflow.Node{{
		Name:           op.Name(),
		PermissionName: socialcontract.AgentName,
		Version:        1,
		Description:    op.Description(),
		ReadOnly:       op.ReadOnly(),
		RetrySafe:      op.RetrySafe(),
		InputSchema:    op.Schema(),
		OutputSchema:   op.OutputSchema(),
		Validate:       op.Validate,
		Resources:      op.Resources,
		Execute:        op.Run,
	}}
}
