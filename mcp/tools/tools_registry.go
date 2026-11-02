package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// registerRegistryReadTools adds the registry-credential probes. They are
// POSTs that only read and test — no wireops state changes — so they are
// annotated readOnly and live in the read group. Authorization is still the
// server's: the underlying route requires the manage_repositories
// capability, so a viewer-scoped key gets its 403 like anywhere else.
func registerRegistryReadTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "test_registry_credential",
		Description: "Verify a SAVED registry credential exactly as persisted: the server decrypts the stored value, runs the registry-v2 handshake (Basic auth, completing a Bearer challenge when offered) and reports stage-structured diagnostics (stage, category, http_status, message, hint). This is the check that catches 'the entered value tested fine but the saved credential fails' — it cannot be fooled by values that were never persisted. The stored value is never revealed or modified. Requires the manage_repositories capability (operator role). A server-side handshake does not prove a worker can pull a specific private image — a pull also exercises worker-side docker auth and repository scope, which this cannot see.",
		Annotations: readOnly(),
	}, testRegistryCredential(c))
}

func testRegistryCredential(c *client.Client) mcp.ToolHandlerFor[models.TestRegistryCredentialInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.TestRegistryCredentialInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		credentialID, err := resolveRegistryCredentialID(ctx, c, apiKey, in.Credential)
		if err != nil {
			return nil, nil, err
		}
		if credentialID == "" {
			return nil, nil, fmt.Errorf("credential is required")
		}
		var out any
		// Persisted mode: the server rejects any draft field alongside
		// credential_id, so the stored value is the only thing this probe can
		// ever exercise — there is no path here that sends a password.
		if err := c.Post(ctx, apiKey, "/api/custom/registry-credentials/test", nil, map[string]any{"credential_id": credentialID}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

// secretPrompter abstracts the MCP elicitation call so the prompting path is
// unit-testable without a live session. *mcp.ServerSession satisfies it.
type secretPrompter interface {
	Elicit(ctx context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error)
}

// resolveSecretValue returns the value to store. An explicitly provided value
// wins; otherwise the user is prompted through a protected elicitation form
// (out of the conversation transcript) when the client supports it. The value
// is only ever returned to the caller of this function — never logged, never
// placed in a _notice, never returned in a tool result.
func resolveSecretValue(ctx context.Context, prompter secretPrompter, provided, key, stackID string) (string, bool, error) {
	if provided != "" {
		return provided, false, nil
	}
	if prompter == nil {
		return "", false, fmt.Errorf("no MCP session is available to prompt for the value — pass value explicitly, understanding it then travels in the tool arguments")
	}
	result, err := prompter.Elicit(ctx, &mcp.ElicitParams{
		Mode:    "form",
		Message: fmt.Sprintf("Enter the secret value for %s on stack %s. It is stored encrypted (internal provider) and can never be read back.", key, stackID),
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value": map[string]any{"type": "string", "description": "Secret value", "minLength": 1},
			},
			"required": []string{"value"},
		},
	})
	if err != nil {
		return "", false, fmt.Errorf("could not prompt for the value (the MCP client may not support elicitation): %w — pass value explicitly if acceptable, knowing it then travels in the tool arguments", err)
	}
	if result.Action != "accept" {
		return "", false, fmt.Errorf("the value prompt was %s — nothing was written", result.Action)
	}
	value, _ := result.Content["value"].(string)
	if value == "" {
		return "", false, fmt.Errorf("the value prompt came back empty — nothing was written")
	}
	return value, true, nil
}

func setStackSecret(c *client.Client) mcp.ToolHandlerFor[models.SetStackSecretInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in models.SetStackSecretInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		stackID, err := resolveStackInput(ctx, c, apiKey, in.StackID, in.Stack)
		if err != nil {
			return nil, nil, err
		}
		if in.Key == "" {
			return nil, nil, fmt.Errorf("key is required")
		}
		var prompter secretPrompter
		if req != nil && req.Session != nil {
			prompter = req.Session
		}
		value, elicited, err := resolveSecretValue(ctx, prompter, in.Value, in.Key, stackID)
		if err != nil {
			return nil, nil, err
		}
		path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/env-vars/bulk"
		body := map[string]any{"mode": "append", "vars": []map[string]any{{"key": in.Key, "value": value, "secret": true}}}
		var out any
		if err := c.Post(ctx, apiKey, path, nil, body, &out); err != nil {
			return nil, nil, err
		}
		// Metadata only: the value appears in neither the result nor any
		// _notice, and the server's audit records field names, never values.
		return nil, map[string]any{
			"stack_id":        stackID,
			"key":             in.Key,
			"secret":          true,
			"secret_provider": "internal",
			"elicited":        elicited,
			"note":            "stored encrypted; it can never be read back. Changing secrets does not redeploy by itself — call deploy_stack to apply.",
		}, nil
	}
}
