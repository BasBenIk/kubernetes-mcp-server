# MCP Prompts Support

The Kubernetes MCP Server supports [MCP Prompts](https://modelcontextprotocol.io/docs/concepts/prompts), which provide pre-defined workflow templates and guidance to AI assistants.

## What are MCP Prompts?

MCP Prompts are pre-defined templates that guide AI assistants through specific workflows. They combine:
- **Structured guidance**: Step-by-step instructions for common tasks
- **Parameterization**: Arguments that customize the prompt for specific contexts
- **Conversation templates**: Pre-formatted messages that guide the interaction

## Creating Custom Prompts

Define custom prompts in your `config.toml` file - no code changes or recompilation needed!

### Example

```toml
[[prompts]]
name = "check-pod-logs"
title = "Check Pod Logs"
description = "Quick way to check pod logs"

[[prompts.arguments]]
name = "pod_name"
description = "Name of the pod"
required = true

[[prompts.arguments]]
name = "namespace"
description = "Namespace of the pod"
required = false

[[prompts.messages]]
role = "user"
content = "Show me the logs for pod {{pod_name}} in {{namespace}}"

[[prompts.messages]]
role = "assistant"
content = "I'll retrieve and analyze the logs for you."
```

## Configuration Reference

### Prompt Fields
- **name** (required): Unique identifier for the prompt
- **title** (optional): Human-readable display name
- **description** (required): Brief explanation of what the prompt does
- **arguments** (optional): List of parameters the prompt accepts
- **messages** (required): Conversation template with role/content pairs

### Argument Fields
- **name** (required): Argument identifier
- **description** (optional): Explanation of the argument's purpose
- **required** (optional): Whether the argument must be provided (default: false)

### Argument Substitution
Use `{{argument_name}}` placeholders in message content. The template engine replaces these with actual values when the prompt is called. If an optional argument is not provided, its placeholder is removed from the output.

## Built-in Prompts

The Kubernetes MCP Server includes several built-in prompts that are always available:

### `cluster-health-check`

Assesses cluster-wide Kubernetes health using bounded, concurrent diagnostic collection.
The prompt limits each resource list and the final report, so a large or partially
unavailable cluster produces actionable partial evidence rather than an unbounded prompt.

**Arguments:**
- `check_events` (optional): Include recent warning/error events in the analysis. Values: `true` or `false`. Default: `true`.

**What it checks:**
- **Nodes**: Status and conditions (Ready, MemoryPressure, DiskPressure, etc.)
- **Pods**: Phase, container statuses, restart counts, and common issues (CrashLoopBackOff, ImagePullBackOff, etc.)
- **Workload Controllers**: Deployments, StatefulSets, and DaemonSets replica status
- **Persistent Volume Claims**: Binding status
- **Events**: Recent warning and error events from the last hour in a bounded set of system namespaces

**Example usage:**
```
Check the health of my cluster
```

You can also skip event checking for faster results:
```
Check the health of my cluster without events
```

### `namespace-health-check`

Assesses workload health within one required namespace. It checks Pods, Deployments,
StatefulSets, DaemonSets, PersistentVolumeClaims, and optionally recent warning/error
events. It never falls back to a cluster-wide scan if the requested namespace is missing
or inaccessible.

**Arguments:**
- `namespace` (required): Namespace to assess.
- `check_events` (optional): Include recent warning/error events. Values: `true` or `false`. Default: `true`.

**Example usage:**
```
Check the health of namespace production
```

Both prompts gather diagnostic data and present it to the LLM for analysis, which will provide:
1. Overall health status (Healthy, Warning, or Critical)
2. Critical issues requiring immediate attention
3. Warnings and recommendations
4. Summary by component

## Configuration File Location

Place your prompts in the `config.toml` file used by the MCP server. Specify the config file path using the `--config` flag when starting the server.

## Toolset Prompts

Toolsets can provide built-in prompts by implementing the `GetPrompts()` method. This allows toolset developers to ship workflow templates alongside their tools.

### Implementing Toolset Prompts

```go
func (t *MyToolset) GetPrompts() []api.ServerPrompt {
    return []api.ServerPrompt{
        {
            Prompt: api.Prompt{
                Name:        "my-workflow",
                Description: "Custom workflow for my toolset",
                Arguments: []api.PromptArgument{
                    {
                        Name:        "namespace",
                        Description: "Target namespace",
                        Required:    true,
                    },
                },
            },
            Handler: func(params api.PromptHandlerParams) (*api.PromptCallResult, error) {
                args := params.GetArguments()
                namespace := args["namespace"]

                // Build messages dynamically based on arguments
                messages := []api.PromptMessage{
                    {
                        Role: "user",
                        Content: api.PromptContent{
                            Type: "text",
                            Text: fmt.Sprintf("Help me with namespace: %s", namespace),
                        },
                    },
                }

                return api.NewPromptCallResult("Workflow description", messages, nil), nil
            },
        },
    }
}
```

### Prompt Merging

When both toolset and config prompts exist:
- Config-defined prompts **override** toolset prompts with the same name
- This allows administrators to customize built-in workflows
- Prompts with unique names from both sources are available
