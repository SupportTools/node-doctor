# Agent Squad Usage Guide

This guide explains how to work with the Multi-Agent Squad system for optimal development workflow.

## Agent Squad Overview

The system includes 17 specialized agents organized by category:

### Architecture (1 agent)
- **Solution Architect**: System design, technology choices, scalability planning

### Engineering (2 agents)
- **Senior Backend Engineer**: API development, database design, backend services
- **Senior Frontend Engineer**: UI/UX implementation, frontend architecture

### Management (1 agent)
- **Dev Task Lifecycle Manager**: Task orchestration, workflow management, progress tracking

### Operations (1 agent)
- **DevOps Engineer**: CI/CD, infrastructure, deployments, monitoring

### Orchestration (1 agent)
- **Prime Orchestrator**: Master coordinator, workflow navigation, agent delegation

### Product (1 agent)
- **Product Manager**: Requirements, PRDs, feature planning, user stories

### Quality (5 agents) ⭐ CRITICAL
- **QA Engineer**: Testing strategy, test automation, quality assurance
- **Devils Advocate QA**: Adversarial testing, finding edge cases, challenging assumptions
- **Go/Kubernetes Skeptic**: Go code critique, K8s anti-patterns, best practices enforcement
- **Web Application Skeptic**: Frontend security, performance, accessibility critique
- **Database Skeptic**: Schema design review, query optimization, data integrity

### Specialized (2+ agents)
- **Kubernetes Specialist**: K8s troubleshooting, cluster management
- **Kubernetes/Docker Specialist**: Container optimization, orchestration patterns
- *[Add your custom agents here]*

## How to Use Agents

### Direct Invocation

Ask Claude to use a specific agent:

```
"Have the senior-backend-engineer agent design the API for user authentication"
"Ask the qa-engineer agent to create a test plan for the payment flow"
"Get the devops-engineer agent to review our CI/CD pipeline"
```

### Automatic Delegation

The Prime Orchestrator automatically delegates to appropriate agents based on task type:

```
User: "I need to deploy the new feature to production"
↓
Prime Orchestrator identifies this as a deployment task
↓
Delegates to devops-engineer agent
↓
devops-engineer handles deployment with quality gates
```

### Multi-Agent Workflows

Complex tasks involve multiple agents in sequence:

```
1. product-manager: Define requirements (PRD)
2. solution-architect: Design system architecture
3. senior-backend-engineer: Implement backend
4. qa-engineer: Create and execute tests
5. devils-advocate-qa: Adversarial verification
6. devops-engineer: Deploy to environment
```

## Quality Gate Agents

The 5 quality agents form mandatory gates in the development workflow:

### 1. QA Engineer (Constructive Testing)
- **When**: After implementation, before deployment
- **Purpose**: Verify functionality, create test plans, ensure quality
- **Blocks**: Deployment if critical issues found

### 2. Devils Advocate QA (Adversarial Testing)
- **When**: After QA Engineer approval, before final deployment
- **Purpose**: Challenge assumptions, find edge cases, stress test
- **Blocks**: Task completion if fundamental issues found

### 3. Go/Kubernetes Skeptic (Technical Critique)
- **When**: During code review for Go/K8s code
- **Purpose**: Enforce best practices, identify anti-patterns
- **Blocks**: Merge if patterns violate standards

### 4. Web Application Skeptic (Frontend Critique)
- **When**: During frontend implementation review
- **Purpose**: Security, performance, accessibility review
- **Blocks**: Deployment if critical issues found

### 5. Database Skeptic (Data Integrity)
- **When**: During schema design or query implementation
- **Purpose**: Data integrity, performance, migration safety
- **Blocks**: Deployment if data risks identified

## Agent Communication Patterns

### Escalation Pattern
```
engineer (identifies complexity)
  ↓
solution-architect (provides design)
  ↓
engineer (implements design)
```

### Validation Pattern
```
engineer (completes implementation)
  ↓
qa-engineer (tests functionality)
  ↓
devils-advocate-qa (stress tests)
  ↓
approved for deployment
```

### Expert Consultation Pattern
```
engineer (faces kubernetes issue)
  ↓
kubernetes-specialist (provides solution)
  ↓
engineer (applies solution)
```

## Best Practices

### 1. Right Agent for the Task
- Use specialized agents for their domain
- Don't ask backend engineers about UI/UX
- Don't ask DevOps about product requirements

### 2. Sequential vs Parallel
- **Sequential**: Tasks with dependencies (design → implement → test)
- **Parallel**: Independent tasks (multiple service implementations)

### 3. Trust the Process
- Don't skip quality gates
- Let Devils Advocate challenge your work
- Address skeptic feedback seriously

### 4. Document Decisions
- Agents should document their decisions
- Keep context in PROJECT_STATUS.md
- Reference agent recommendations in commits

### 5. Iterate with Feedback
- Use agent feedback to improve
- Re-engage agents after fixes
- Close the feedback loop

## Common Workflows

### Feature Development
```
1. product-manager: Define feature (PRD)
2. solution-architect: Design architecture
3. senior-backend-engineer: Implement backend
4. senior-frontend-engineer: Implement UI
5. qa-engineer: Test integration
6. go-kubernetes-skeptic: Review code quality
7. devils-advocate-qa: Adversarial testing
8. devops-engineer: Deploy to staging
9. [Validation passes]
10. devops-engineer: Deploy to production
```

### Bug Fix
```
1. qa-engineer: Reproduce and document bug
2. senior-backend-engineer: Fix implementation
3. qa-engineer: Verify fix
4. devils-advocate-qa: Ensure no regressions
5. devops-engineer: Deploy hotfix
```

### Infrastructure Change
```
1. devops-engineer: Propose infrastructure change
2. solution-architect: Review design
3. kubernetes-specialist: Validate K8s config
4. devops-engineer: Implement change
5. qa-engineer: Verify system stability
6. devops-engineer: Roll out to production
```

### Architecture Review
```
1. solution-architect: Present design
2. senior-backend-engineer: Review backend implications
3. database-skeptic: Review data model
4. kubernetes-specialist: Review deployment model
5. devops-engineer: Review operational impact
```

## Agent Customization

### Adding Project-Specific Agents

1. Create agent file in `.claude/agents/specialized/`
2. Update `agent-registry.json`
3. Document in this guide
4. Test with sample tasks

### Modifying Agent Behavior

Agents can be tuned by:
- Updating their definition files
- Adding project-specific context
- Including domain-specific expertise
- Adjusting communication style

## Troubleshooting

### Agent Not Responding Appropriately
- Check agent definition for clarity
- Verify task is within agent expertise
- Ensure sufficient context provided

### Conflicting Agent Recommendations
- Escalate to solution-architect
- Document trade-offs
- Make informed decision with team
- Record rationale in PROJECT.md

### Quality Gate Blocking Progress
- **Don't skip the gate**
- Address feedback thoroughly
- Re-engage agent after fixes
- If stuck, escalate to architect

## Remember

The agent squad exists to:
- **Enforce quality** through systematic review
- **Distribute expertise** across all tasks
- **Prevent mistakes** before they reach production
- **Accelerate development** through specialization

Trust the process. The gates exist for a reason.
