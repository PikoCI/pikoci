package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/pikoci/pikoci/pikoci/builtin"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/service"
	"github.com/pikoci/pikoci/pikoci/utils"
)

const (
	pikoPrefix = "pikoci://"
	baseURL    = "https://raw.githubusercontent.com/pikoci/pikoci/master/pikoci/builtin"
)

type hclResourceType struct {
	ResourceTypes []restype.ResourceType `hcl:"resource_type,block"`
}

type hclRunner struct {
	Runners []runner.Runner `hcl:"runner_type,block"`
}

type hclSecretType struct {
	SecretTypes []sectype.SecretType `hcl:"secret_type,block"`
}

type hclServiceFile struct {
	Services []hclServiceBlock `hcl:"service_type,block"`
}

type hclServiceBlock struct {
	Name       string                `hcl:"name,label"`
	Source     string                `hcl:"source,optional"`
	Params     []string              `hcl:"params,optional"`
	Start      []utils.RunnerCommand `hcl:"start,block"`
	ReadyCheck []hclReadyCheck       `hcl:"ready_check,block"`
	Stop       []utils.RunnerCommand `hcl:"stop,block"`
}

type hclReadyCheck struct {
	Runner   string            `hcl:"runner,label"`
	Args     []string          `hcl:"args,optional"`
	Interval string            `hcl:"interval,optional"`
	Timeout  string            `hcl:"timeout,optional"`
	Params   map[string]string `hcl:",remain"`
}

func (hs hclServiceBlock) toService() service.Service {
	s := service.Service{
		Name:   hs.Name,
		Source: hs.Source,
		Params: hs.Params,
	}
	if len(hs.Start) > 0 {
		s.Start = hs.Start[0]
	}
	if len(hs.Stop) > 0 {
		s.Stop = hs.Stop[0]
	}
	if len(hs.ReadyCheck) > 0 {
		rc := hs.ReadyCheck[0]
		s.ReadyCheck = &service.ReadyCheck{
			RunnerCommand: utils.RunnerCommand{
				Runner: rc.Runner,
				Args:   rc.Args,
				Params: rc.Params,
			},
			Interval: rc.Interval,
			Timeout:  rc.Timeout,
		}
	}
	return s
}

func ResolveResourceType(ctx context.Context, src string) (*restype.ResourceType, error) {
	data, err := resolveHCL(ctx, src, "resource_types")
	if err != nil {
		return nil, err
	}

	var hrt hclResourceType
	err = hclsimple.Decode("source.hcl", data, nil, &hrt)
	if err != nil {
		return nil, fmt.Errorf("failed to decode resource type from source %q: %w", src, err)
	}
	if len(hrt.ResourceTypes) == 0 {
		return nil, fmt.Errorf("no resource_type block found in source %q", src)
	}
	return &hrt.ResourceTypes[0], nil
}

func ResolveRunner(ctx context.Context, src string) (*runner.Runner, error) {
	data, err := resolveHCL(ctx, src, "runners")
	if err != nil {
		return nil, err
	}

	var hr hclRunner
	err = hclsimple.Decode("source.hcl", data, nil, &hr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode runner from source %q: %w", src, err)
	}
	if len(hr.Runners) == 0 {
		return nil, fmt.Errorf("no runner_type block found in source %q", src)
	}
	return &hr.Runners[0], nil
}

func ResolveSecretType(ctx context.Context, src string) (*sectype.SecretType, error) {
	data, err := resolveHCL(ctx, src, "secret_types")
	if err != nil {
		return nil, err
	}

	var hst hclSecretType
	err = hclsimple.Decode("source.hcl", data, nil, &hst)
	if err != nil {
		return nil, fmt.Errorf("failed to decode secret type from source %q: %w", src, err)
	}
	if len(hst.SecretTypes) == 0 {
		return nil, fmt.Errorf("no secret_type block found in source %q", src)
	}
	return &hst.SecretTypes[0], nil
}

func ResolveService(ctx context.Context, src string) (*service.Service, error) {
	data, err := resolveHCL(ctx, src, "services")
	if err != nil {
		return nil, err
	}

	var hs hclServiceFile
	err = hclsimple.Decode("source.hcl", data, nil, &hs)
	if err != nil {
		return nil, fmt.Errorf("failed to decode service from source %q: %w", src, err)
	}
	if len(hs.Services) == 0 {
		return nil, fmt.Errorf("no service_type block found in source %q", src)
	}
	svc := hs.Services[0].toService()
	return &svc, nil
}

func resolveHCL(ctx context.Context, src, kind string) ([]byte, error) {
	if strings.HasPrefix(src, pikoPrefix) {
		name := strings.TrimPrefix(src, pikoPrefix)
		// Try embedded built-in first
		switch kind {
		case "resource_types":
			if data, ok := builtin.ResourceTypeHCL(name); ok {
				return data, nil
			}
		case "runners":
			if data, ok := builtin.RunnerHCL(name); ok {
				return data, nil
			}
		case "secret_types":
			if data, ok := builtin.SecretTypeHCL(name); ok {
				return data, nil
			}
		case "services":
			if data, ok := builtin.ServiceHCL(name); ok {
				return data, nil
			}
		}
		// Fall back to GitHub raw URL
		url := fmt.Sprintf("%s/%s/%s.hcl", baseURL, kind, name)
		return fetchURL(ctx, url)
	}

	if strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "http://") {
		return fetchURL(ctx, src)
	}

	return nil, fmt.Errorf("unsupported source URL scheme: %q", src)
}

func fetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %q: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %q: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch %q: status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %q: %w", url, err)
	}
	return data, nil
}
