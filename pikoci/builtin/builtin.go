// Package builtin provides access to the built-in resource types, runners,
// and secret types that ship with PikoCI. Definitions are embedded as HCL
// files at compile time and parsed lazily on first access using sync.Once.
package builtin

import (
	"embed"
	"fmt"
	"sync"

	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
)

// Note: notification type HCL files live in notification_types/ but are NOT
// embedded. They are resolved via the pikoci:// source scheme, which falls
// back to the GitHub raw URL when not found in the embedded FS.

//go:embed resource_types/artifact.hcl resource_types/cron.hcl resource_types/git.hcl resource_types/trigger.hcl
var resourceTypeFS embed.FS

//go:embed runners/*.hcl
var runnerFS embed.FS

//go:embed secret_types/*.hcl
var secretTypeFS embed.FS

type hclResourceType struct {
	ResourceTypes []restype.ResourceType `hcl:"resource_type,block"`
}

type hclRunner struct {
	Runners []runner.Runner `hcl:"runner_type,block"`
}

type hclSecretType struct {
	SecretTypes []sectype.SecretType `hcl:"secret_type,block"`
}

var (
	resourceTypes     map[string]restype.ResourceType
	resourceTypesOnce sync.Once

	runners     map[string]runner.Runner
	runnersOnce sync.Once

	secretTypes     map[string]sectype.SecretType
	secretTypesOnce sync.Once
)

// ResourceTypes returns a map of all built-in resource types keyed by name.
// The result is computed once and cached for subsequent calls.
func ResourceTypes() map[string]restype.ResourceType {
	resourceTypesOnce.Do(func() {
		resourceTypes = make(map[string]restype.ResourceType)
		entries, err := resourceTypeFS.ReadDir("resource_types")
		if err != nil {
			panic(fmt.Sprintf("failed to read embedded resource_types: %v", err))
		}
		for _, e := range entries {
			data, err := resourceTypeFS.ReadFile("resource_types/" + e.Name())
			if err != nil {
				panic(fmt.Sprintf("failed to read embedded resource_type %s: %v", e.Name(), err))
			}
			var hrt hclResourceType
			err = hclsimple.Decode(e.Name(), data, nil, &hrt)
			if err != nil {
				panic(fmt.Sprintf("failed to decode embedded resource_type %s: %v", e.Name(), err))
			}
			for _, rt := range hrt.ResourceTypes {
				resourceTypes[rt.Name] = rt
			}
		}
	})
	return resourceTypes
}

// Runners returns a map of all built-in runners keyed by name. The result is
// computed once and cached for subsequent calls.
func Runners() map[string]runner.Runner {
	runnersOnce.Do(func() {
		runners = make(map[string]runner.Runner)
		entries, err := runnerFS.ReadDir("runners")
		if err != nil {
			panic(fmt.Sprintf("failed to read embedded runners: %v", err))
		}
		for _, e := range entries {
			data, err := runnerFS.ReadFile("runners/" + e.Name())
			if err != nil {
				panic(fmt.Sprintf("failed to read embedded runner %s: %v", e.Name(), err))
			}
			var hr hclRunner
			err = hclsimple.Decode(e.Name(), data, nil, &hr)
			if err != nil {
				panic(fmt.Sprintf("failed to decode embedded runner %s: %v", e.Name(), err))
			}
			for _, ru := range hr.Runners {
				runners[ru.Name] = ru
			}
		}
	})
	return runners
}

// ResourceTypeHCL returns the raw HCL bytes for a built-in resource type, if it exists.
func ResourceTypeHCL(name string) ([]byte, bool) {
	data, err := resourceTypeFS.ReadFile("resource_types/" + name + ".hcl")
	if err != nil {
		return nil, false
	}
	return data, true
}

// SecretTypes returns a map of all built-in secret types keyed by name. The
// result is computed once and cached for subsequent calls.
func SecretTypes() map[string]sectype.SecretType {
	secretTypesOnce.Do(func() {
		secretTypes = make(map[string]sectype.SecretType)
		entries, err := secretTypeFS.ReadDir("secret_types")
		if err != nil {
			panic(fmt.Sprintf("failed to read embedded secret_types: %v", err))
		}
		for _, e := range entries {
			data, err := secretTypeFS.ReadFile("secret_types/" + e.Name())
			if err != nil {
				panic(fmt.Sprintf("failed to read embedded secret_type %s: %v", e.Name(), err))
			}
			var hst hclSecretType
			err = hclsimple.Decode(e.Name(), data, nil, &hst)
			if err != nil {
				panic(fmt.Sprintf("failed to decode embedded secret_type %s: %v", e.Name(), err))
			}
			for _, st := range hst.SecretTypes {
				secretTypes[st.Name] = st
			}
		}
	})
	return secretTypes
}

// SecretTypeHCL returns the raw HCL bytes for a built-in secret type, if it exists.
func SecretTypeHCL(name string) ([]byte, bool) {
	data, err := secretTypeFS.ReadFile("secret_types/" + name + ".hcl")
	if err != nil {
		return nil, false
	}
	return data, true
}

// RunnerHCL returns the raw HCL bytes for a built-in runner, if it exists.
func RunnerHCL(name string) ([]byte, bool) {
	data, err := runnerFS.ReadFile("runners/" + name + ".hcl")
	if err != nil {
		return nil, false
	}
	return data, true
}

// NotificationTypes returns an empty map. No notification types are embedded
// in the binary. Use source = "pikoci://<name>" to resolve them from the
// PikoCI registry.
func NotificationTypes() map[string]notiftype.NotificationType {
	return nil
}

// NotificationTypeHCL returns nil. Notification type HCL files are not
// embedded; the pikoci:// source scheme falls back to the GitHub raw URL.
func NotificationTypeHCL(name string) ([]byte, bool) {
	return nil, false
}

// ServiceHCL returns the raw HCL bytes for a built-in service, if it exists.
// No built-in services are shipped yet, but this supports the source resolution
// pipeline for future additions and for https:// sources.
func ServiceHCL(name string) ([]byte, bool) {
	return nil, false
}
