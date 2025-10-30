# Master Task Management System

The {{PROJECT_NAME}} project follows a prioritized task management system based on the master design documents. ALL development work must follow these rules for controlled, systematic progress.

## Task Reference Documents

- **Master Design Document**: `/master-design-doc.md` - System architecture and service dependencies
- **Task Management System**: TaskForge project management system with 546 tasks organized in hierarchical structure
  - Tasks organized: Project → Features → Tasks (11 features, 546 total tasks)
  - Accessible via MCP tools: `mcp__taskforge__getTasks`, `mcp__taskforge__createTask`, `mcp__taskforge__updateTask`
  - See [TaskForge Usage Guide](taskforge-usage-guide.md) for complete documentation
  - Migration from todo-mcp documented in `taskforge-migration-guide.md`

## Priority System (MUST FOLLOW)

### Priority 1 - Foundation Services (IMPLEMENT FIRST)

- org-management-controller: Core orchestration system
- notification-service: Required by all alert systems
- Database consolidation: Multi-tenant and single-tenant patterns
- **BLOCKER**: No P2+ work until P1 foundation is complete

### Priority 2 - Core Monitoring (BUSINESS CRITICAL)

- alert-controller: Internal monitoring alerts
- health-controller: Platform health monitoring
- linux-agent: Enhanced data collection
- **DEPENDENCY**: Requires P1 foundation services

### Priority 3 - Advanced Features (VALUE-ADD)

- probe-controller, tag-controller, job-controller, scheduler-controller
- **DEPENDENCY**: Requires P1 + P2 core services

### Priority 4 - Business Operations (REVENUE)

- billing-controller, reporting-controller
- **DEPENDENCY**: Requires monitoring infrastructure from P1-P3

### Priority 5 - Platform Polish (UX)

- Web-UI critical fixes (P5A) then enhancements (P5B)
- **PARALLEL**: Can work on P5A critical fixes alongside P1-P2

## Task Execution Rules

### Before Starting ANY Task

Use TaskForge to select the next task:

```javascript
// 1. List tasks for current priority using TaskForge
mcp__taskforge__getTasks({
  projectId: 1,
  featureId: 1,              // P1-Foundation-Services
  status: "todo",
  priority: "high"
});

// 2. Check for blocked tasks
mcp__taskforge__getBlockedTasks({
  projectId: 1
});

// 3. Use TodoWrite tool to track session progress
```

**Task Selection Rules:**

1. **NEVER skip priorities** - Complete P1 before P2, P2 before P3, etc.
2. **RESPECT dependencies** - Check [DEPENDS ON: service] annotations
3. **ONE service at a time** - Complete service implementation before starting next
4. **CRITICAL first** - Within priority, do CRITICAL before HIGH before MEDIUM

### Task Status Tracking (MANDATORY)

Using TaskForge (PostgreSQL-backed project management system):

```javascript
// 1. Start a task - set status to "in_progress"
mcp__taskforge__updateTask({
  taskId: 123,
  status: "in_progress"
});

// 2. Complete a task
mcp__taskforge__updateTask({
  taskId: 123,
  status: "done",
  actualHours: 6.5
});

// 3. Block a task (for dependencies or issues)
mcp__taskforge__updateTask({
  taskId: 123,
  status: "blocked"
});
// Or use mcp__taskforge__setDependency to track blocking tasks

// 4. Delete irrelevant tasks (soft delete)
mcp__taskforge__deleteTask({
  taskId: 123
});

// 5. Use TodoWrite tool for session-level progress tracking
```

## Implementation Standards

### For Backend Services (Go)

```bash
# ALWAYS follow this sequence:
# 1. Create service directory structure
mkdir -p controllers/[service-name]/{cmd,pkg/{handlers,models,services}}

# 2. Implement core functionality with proper patterns
# - Single-function-per-file handlers
# - Comprehensive logging (Trace/Debug)
# - Complete Swagger documentation
# - Database models in BOTH locations (ConnectDB.go AND migrator.go)

# 3. Test locally before committing
make build-api-local
make test-api-local
go test -v ./controllers/[service-name]/...

# 4. Update TaskForge with completion
```

### For Multi-Service Integration

```bash
# ALWAYS verify service dependencies:
# 1. notification-service running before alert-controller
# 2. org-management-controller before single-tenant services
# 3. Database schema supports both single/multi-tenant patterns

# Test integration points:
# - API endpoints responding
# - Database connections working
# - Service-to-service communication functional
```

### Deployment Integration

```bash
# Each service MUST include:
# 1. Dockerfile in service directory
# 2. Helm templates in helm/server/templates/<service-name>/
# 3. Helm chart integration with parent chart (values.yaml)
# 4. CI/CD pipeline integration

# Test deployment locally:
# Build → Test → Deploy to dev → Validate
```

## Controlled Development Workflow

### Phase 1: Foundation (P1 Tasks Only)

1. org-management-controller core API
2. notification-service with multi-tenant support
3. Database schema consolidation
4. **GATE**: Foundation integration testing before P2

### Phase 2: Core Monitoring (P2 Tasks)

1. alert-controller with notification integration
2. health-controller with org-management integration
3. linux-agent enhancements
4. **GATE**: End-to-end monitoring workflow before P3

### Phase 3: Advanced Features (P3 Tasks)

1. probe-controller, tag-controller implementation
2. job-controller and scheduler-controller
3. **GATE**: Full platform feature testing before P4

### Phase 4: Business Operations (P4 Tasks)

1. billing-controller with notification integration
2. reporting-controller with data aggregation
3. **GATE**: Business workflow testing before P5B

### Phase 5: Platform Polish

1. P5A: Critical web-UI fixes (parallel with P1-P2)
2. P5B: Web-UI enhancements (after P1-P4 complete)

## Quality Gates (MANDATORY)

### Before Moving to Next Priority

```bash
# 1. All tasks in current priority marked [COMPLETED]
# 2. Integration tests passing for completed services
# 3. Local deployment successful
# 4. No critical bugs or blockers
# 5. Documentation updated

# Validation commands:
make build-all-local
make test-local
./scripts/validate-all.sh
```

### Service Completion Checklist

- [ ] Core functionality implemented and tested
- [ ] Database models added to both locations
- [ ] API endpoints with Swagger docs
- [ ] Service integration tests passing
- [ ] Helm chart and deployment configs
- [ ] TaskForge updated with completion
- [ ] No critical or high-priority bugs
- [ ] Ready for next service dependencies

## Emergency Procedures

### If Blocked on Current Priority

1. Document blocker in TaskForge
2. Create specific blocker resolution tasks
3. **NEVER jump to next priority** - resolve blocker first
4. Consider parallel work on P5A critical fixes only

### If Service Integration Fails

1. Roll back to last working state
2. Create integration debugging tasks
3. Fix integration before continuing with service
4. Update master design doc if architecture changes needed

## Task Development Workflow

**ALL development tasks MUST follow the comprehensive workflow documented in `task-execution-workflow.md`.**

This mandatory 8-step workflow includes:

1. **Task Selection & Research** - Mandatory research phase before any coding
2. **Design & Planning** - Present comprehensive implementation plan
3. **Approval Gate** - Wait for explicit approval before coding
4. **Implementation** - Code with strict adherence to standards
5. **Quality Assurance** - Build verification, QA review, and testing
6. **Deployment** - Mandatory deployment with `make bump`
7. **Devils Advocate Verification** - Final validation of task completion
8. **Task Completion** - Update TaskForge and commit changes

**Key Requirements:**
- ONE task at a time - never work on multiple tasks simultaneously
- RESEARCH before coding - no exceptions
- BUILD verification required before QA review
- QA validation required before deployment
- DEPLOYMENT mandatory before completion
- DEVILS ADVOCATE approval required before marking complete
- Multi-repository updates must be completed together

**Multi-Repository Considerations:**

When tasks affect multiple repositories ({{PROJECT_NAME}}, go-sdk, linux-agent, web-ui, cli):
- Update repositories in dependency order: API → SDK → Agent/UI/CLI
- Build and test ALL modified repositories
- Commit and tag ALL repositories appropriately
- NEVER leave repositories in inconsistent state

See [Task Execution Workflow](task-execution-workflow.md) for complete details, examples, and enforcement rules.

## See Also

- [TaskForge Usage Guide](taskforge-usage-guide.md) - Complete TaskForge MCP tools documentation
- [Task Execution Workflow](task-execution-workflow.md) - Mandatory 8-step development workflow
- [go-sdk Synchronization Guide](go-sdk-synchronization-guide.md) - SDK update requirements
- [Web UI-API Synchronization](web-ui-api-synchronization.md) - Frontend sync procedures
- [Helm Chart Standards](helm-chart-standards.md) - Deployment configuration rules
