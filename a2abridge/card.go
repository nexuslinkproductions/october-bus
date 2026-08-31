package a2abridge

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/october-dev/october-bus/bus"
)

const bearerSchemeName a2a.SecuritySchemeName = "bearer"

type CardOptions struct {
	InterfaceURL string
	Version      string
	Description  string
}

func NewAgentCard(agent bus.Agent, options CardOptions) (*a2a.AgentCard, error) {
	if strings.TrimSpace(agent.DisplayName) == "" {
		return nil, errors.New("agent display name is required")
	}
	if err := validateInterfaceURL(options.InterfaceURL); err != nil {
		return nil, err
	}
	if options.Version == "" {
		options.Version = bus.Version
	}
	if options.Description == "" {
		options.Description = "AI agent connected through October Bus."
	}

	skills := make([]a2a.AgentSkill, 0, len(agent.Capabilities))
	for _, capability := range agent.Capabilities {
		description := capability.Description
		if description == "" {
			description = "Supports " + capability.Name + "."
		}
		skills = append(skills, a2a.AgentSkill{
			ID:          capability.Name,
			Name:        capability.Name,
			Description: description,
			Tags:        []string{capability.Name},
		})
	}

	return &a2a.AgentCard{
		Name:        agent.DisplayName,
		Description: options.Description,
		Version:     options.Version,
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(options.InterfaceURL, a2a.TransportProtocolHTTPJSON),
		},
		Capabilities:       a2a.AgentCapabilities{},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills:             skills,
		SecuritySchemes: a2a.NamedSecuritySchemes{
			bearerSchemeName: a2a.HTTPAuthSecurityScheme{
				Scheme:      "Bearer",
				Description: "October Bus agent credential.",
			},
		},
		SecurityRequirements: a2a.SecurityRequirementsOptions{
			{bearerSchemeName: a2a.SecuritySchemeScopes{}},
		},
	}, nil
}

func validateInterfaceURL(value string) error {
	if len(value) > 4096 {
		return errors.New("A2A interface URL exceeds 4096 bytes")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("A2A interface URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("A2A interface URL must not include credentials, a query, or a fragment")
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return errors.New("non-loopback A2A interfaces require HTTPS")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
