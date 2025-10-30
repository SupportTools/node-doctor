# Specialized Agents Directory

This directory contains project-specific expert agents tailored to your technology stack and domain.

## Included Specialized Agents

- **kubernetes-specialist.md** - Kubernetes cluster management and troubleshooting expert
- **kubernetes-docker-specialist.md** - Container orchestration and Docker optimization expert

## Adding Custom Agents

To add a specialized agent for your project:

### 1. Create Agent Definition

Create a new file in this directory (e.g., `my-expert.md`):

```markdown
# My Domain Expert

Expert in [specific technology/domain] with deep knowledge of [specific areas].

## Expertise

- Area 1
- Area 2
- Area 3

## When to Use

Use this agent when:
- Scenario 1
- Scenario 2

## Tools Available

- Tool1
- Tool2

## Example Usage

[Provide examples of how to invoke this agent]
```

### 2. Update Agent Registry

Add your agent to `.claude/agents/agent-registry.json`:

```json
{
  "agents": [
    {
      "id": "my-expert",
      "name": "My Domain Expert",
      "category": "specialized",
      "file": "specialized/my-expert.md",
      "description": "Expert in specific domain",
      "skills": ["skill1", "skill2"]
    }
  ]
}
```

### 3. Update CLAUDE.md

Add reference to your agent in the root `CLAUDE.md` if it should be part of core workflows.

## Agent Template

Use this template for creating new specialized agents:

```markdown
# [Agent Name]

[One-sentence description of expertise]

You are [Agent Name], a highly experienced [role/specialization] with [X years] of expertise in [domain/technology].

## Core Expertise

- **[Area 1]**: [Description]
- **[Area 2]**: [Description]
- **[Area 3]**: [Description]

## Responsibilities

1. [Primary responsibility]
2. [Secondary responsibility]
3. [Additional responsibilities...]

## When to Engage Me

Engage me when you need to:
- [Scenario 1]
- [Scenario 2]
- [Scenario 3]

## My Approach

I follow these principles:
1. [Principle 1]
2. [Principle 2]
3. [Principle 3]

## Tools I Use

[List of tools this agent has access to or specializes in]

## Communication Style

[How this agent communicates - formal, technical, collaborative, etc.]

## Example Invocations

**Scenario 1:**
```
User: [Example request]
Agent: [Example response approach]
```

**Scenario 2:**
```
User: [Example request]
Agent: [Example response approach]
```

## Quality Standards

I ensure:
- [Standard 1]
- [Standard 2]
- [Standard 3]

## Collaboration

I work closely with:
- [Related Agent 1] for [purpose]
- [Related Agent 2] for [purpose]
```

## Examples from Nexmonyx

The Nexmonyx project includes these specialized agents:

- **Rancher Expert** - Rancher API integration and cluster management
- **RUCC Controller Expert** - Rancher Upgrade Check Controller specialist
- **Upgrade Safety Specialist** - Kubernetes upgrade safety and validation

You can find examples of these in the nexmonyx repository for reference when creating your own.

## Best Practices

1. **Focus**: Each agent should have a narrow, deep focus
2. **Expertise**: Provide specific, actionable knowledge
3. **Integration**: Ensure agents work well with the core agent squad
4. **Documentation**: Clearly document when and how to use the agent
5. **Testing**: Test agent prompts before adding to production workflows

## Agent Naming

Follow these naming conventions:

- Use descriptive names: `kubernetes-specialist` not `k8s-guy`
- Use hyphens for multi-word names
- Include domain or technology: `react-performance-expert`
- Avoid generic names: `helper`, `utility`, `misc`

## Maintenance

Review and update specialized agents:
- When technology versions change
- When best practices evolve
- When team expertise grows
- When new tools are adopted
