package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

// Authorization binds the executable contract and exact arguments, never presentation IDs.
type authorization struct {
	id      string
	tool    string
	version int
	args    string
}

func authorizationFor(s workflow.Step) authorization {
	v := s.NodeVersion
	if v == 0 {
		v = 1
	}

	return authorization{s.ID, s.Tool, v, string(s.Args)}
}

type handler struct {
	permission sync.Mutex // serializes interactive authorization across map workers
	artifacts  func(string) workflow.ArtifactAccess
	registry   *workflow.Registry
	authorize  func(context.Context, string, json.RawMessage) error
	authorized map[authorization]bool
	templates  bool
}

func (h *handler) Validate(s workflow.Step) error {
	n, err := h.registry.Node(s.Tool)
	if err != nil {
		return err
	}

	version := s.NodeVersion
	if version == 0 {
		version = 1
	}

	if version != n.Version {
		return fmt.Errorf("node %s version %d unavailable (registered %d)", s.Tool, version, n.Version)
	}
	if s.Repeat != nil {
		err = recipe.ValidateRepeat(s)
		if err != nil {
			return err
		}
	}
	if s.Tool == "workflow_call" && len(s.ForEach) == 0 {
		err = h.validateCall(s)
		if err != nil {
			return err
		}
	}

	if s.Concurrency < 0 || s.Concurrency > workflow.MaxConcurrency {
		return fmt.Errorf("concurrency must be between 1 and %d when supplied", workflow.MaxConcurrency)
	}
	if s.OnError != "" && s.OnError != "stop" && s.OnError != "collect" {
		return fmt.Errorf("on_error must be stop or collect")
	}
	if len(s.ForEach) == 0 && (s.Concurrency != 0 || s.OnError != "") {
		return fmt.Errorf("concurrency and on_error require for_each")
	}
	if s.Concurrency > 1 && !n.Concurrent {
		return fmt.Errorf("node %s does not permit concurrent invocations", s.Tool)
	}

	if s.Retry != nil {
		if s.Retry.MaxAttempts < 1 || s.Retry.MaxAttempts > 3 || s.Retry.DelayMS < 0 || s.Retry.DelayMS > 10000 {
			return fmt.Errorf("invalid retry limits")
		}

		if s.Retry.MaxAttempts > 1 && !n.RetrySafe {
			return fmt.Errorf("node %s does not permit retries", s.Tool)
		}
	}

	if len(s.ForEach) > 0 {
		if s.Tool == "workflow_call" {
			if s.Child == nil || s.Retry != nil {
				return fmt.Errorf("mapped call requires a frozen child and cannot retry")
			}
			err = workflow.ValidateJSON(s.Args, n.InputSchema)
			if err != nil {
				return err
			}
			var base workflow.CallInput
			err = decode(s.Args, &base)
			if err != nil {
				return err
			}
			if len(base.Inputs) != 0 {
				return fmt.Errorf("mapped calls require inputs in for_each, not args")
			}
		} else {
			var base map[string]json.RawMessage
			err = json.Unmarshal(s.Args, &base)
			if err != nil || len(base) != 0 {
				return fmt.Errorf("for_each requires empty args; each item is a complete argument object")
			}
		}

		if h.templates && recipe.HasOutputReferences(s.ForEach) {
			return nil
		}

		var items []json.RawMessage
		if err := json.Unmarshal(s.ForEach, &items); err != nil || string(s.ForEach) == "null" || len(items) > workflow.MaxIterations {
			return fmt.Errorf("for_each must be an array of at most %d argument objects", workflow.MaxIterations)
		}

		s.ForEach = nil
		s.Concurrency = 0
		s.OnError = ""

		for _, item := range items {
			child := s
			if s.Tool == "workflow_call" {
				child, err = recipe.CallIteration(s, item)
			} else {
				child.Args = item
			}
			if err != nil {
				return err
			}
			err = h.Validate(child)
			if err != nil {
				return err
			}
		}

		return nil
	}

	if h.templates {
		if n.ValidateTemplate != nil {
			if err := n.ValidateTemplate(s.Args); err != nil {
				return err
			}
		}

		if recipe.HasOutputReferences(s.Args) {
			return workflow.ValidateJSON(s.Args, workflow.TemplateSchema(n.InputSchema))
		}
	}

	if err := workflow.ValidateJSON(s.Args, n.InputSchema); err != nil {
		return err
	}

	if n.Validate != nil {
		return n.Validate(s.Args)
	}

	return nil
}

func (h *handler) Run(ctx context.Context, s workflow.Step) (string, error) {
	n, err := h.registry.Node(s.Tool)
	if err != nil {
		return "", err
	}

	if !h.authorized[authorizationFor(s)] {
		err = h.authorizeCall(ctx, s)
		if err != nil {
			return "", err
		}
	}

	if h.artifacts != nil {
		ctx = workflow.WithArtifacts(ctx, h.artifacts(s.ID))
	}

	out, err := n.Execute(ctx, s.Args)
	if err != nil {
		return out, err
	}
	if len(n.OutputSchema) > 0 {
		err = workflow.ValidateJSON(json.RawMessage(out), n.OutputSchema)
	}

	return out, err
}

func (h *handler) authorizeCall(ctx context.Context, s workflow.Step) error {
	h.permission.Lock()
	defer h.permission.Unlock()

	err := ctx.Err()
	if err != nil {
		return err
	}
	return h.authorize(ctx, s.Tool, s.Args)
}

func (t Service) authorizeNode(ctx context.Context, name string, args json.RawMessage) error {
	n, err := t.Registry.Node(name)
	if err != nil {
		return err
	}

	if n.Internal && n.PermissionName == "" {
		return nil
	}

	if n.PermissionName != "" {
		name = n.PermissionName
	}

	resources, err := n.Resources(args)
	if err != nil {
		return err
	}

	if t.AuthorizeResource == nil {
		if t.Authorize != nil {
			return t.Authorize(ctx, name, args)
		}

		return fmt.Errorf("node resource authorization unavailable")
	}

	if len(resources) == 0 {
		resources = []string{""}
	}

	for _, resource := range resources {
		if err := t.AuthorizeResource(ctx, name, resource, args); err != nil {
			return err
		}
	}

	return nil
}
