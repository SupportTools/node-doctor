# Custom Prompts Directory

This directory contains custom prompt templates that can be used with the Multi-Agent Squad system.

## Purpose

Custom prompts allow you to:
- Create reusable prompt templates
- Standardize common workflows
- Share prompt patterns across the team
- Extend agent capabilities with specialized prompts

## Usage

Create prompt files in this directory and reference them in your agent definitions or workflow documentation.

### Example Prompt Template

```markdown
# Feature Implementation Prompt

You are implementing a new feature with the following requirements:

## Requirements
{requirements}

## Acceptance Criteria
{acceptance_criteria}

## Technical Constraints
{technical_constraints}

## Steps to Follow

1. Review requirements thoroughly
2. Design the implementation approach
3. Get architect approval
4. Implement with tests
5. Submit for QA review
6. Deploy after approval

## Quality Standards

- All code must have tests
- Documentation must be updated
- API changes must update OpenAPI spec
- Follow project coding standards
```

## Best Practices

1. **Parameterize**: Use `{variable}` syntax for dynamic content
2. **Structure**: Include clear sections and steps
3. **Context**: Provide necessary context and constraints
4. **Quality**: Include quality standards and acceptance criteria
5. **Examples**: Provide examples where helpful

## Organization

Organize prompts by:
- **workflow/**: Workflow-specific prompts
- **quality/**: Quality gate prompts
- **development/**: Development task prompts
- **operations/**: Operational task prompts

## Integration

Prompts can be referenced in:
- Agent definitions (`.claude/agents/`)
- Workflow documentation (`docs/workflows/`)
- Task execution templates
- Slash commands

## Examples

See the Nexmonyx repository for examples of custom prompts used in production workflows.
