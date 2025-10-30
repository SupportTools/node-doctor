# Documentation Hub

Central navigation for all project documentation.

## 🚀 Quick Start

New to the project? Start here:

1. **[Root CLAUDE.md](../CLAUDE.md)** - Multi-agent orchestration overview
2. **[.claude/AGENT_GUIDE.md](../.claude/AGENT_GUIDE.md)** - How to use the 17 agents
3. **[Task Execution Workflow](development/task-execution-workflow.md)** - Mandatory development process
4. **[Template README](../README.md)** - Template usage guide

## 📚 Core Documentation

### Development Standards
**Location**: `docs/development/`

Essential reading for all developers:

- **[Task Execution Workflow](development/task-execution-workflow.md)** ⭐ MANDATORY - 8-step process
- **[Master Task Management](development/master-task-management.md)** ⭐ Priority system (P1-P5)
- **[TaskForge Usage Guide](development/taskforge-usage-guide.md)** - Project management system
- **[API Handler Architecture](development/api-handler-architecture-standards.md)** - Handler patterns
- **[Helm Chart Standards](development/helm-chart-standards.md)** - Kubernetes deployment
- **[Local CI/CD Validation](development/local-cicd-validation-guide.md)** - Pre-push validation
- **[Database Patterns Guide](development/database-patterns-guide.md)** - Multi-tenant vs single-tenant
- **[Error Response Standard](development/error-response-standard.md)** - API error handling
- **[Web UI-API Synchronization](development/web-ui-api-synchronization.md)** - Frontend sync
- **[Go SDK Synchronization](development/go-sdk-synchronization-guide.md)** - SDK updates

### Workflows
**Location**: `docs/workflows/`

Standard process workflows:

- **[Feature Development](workflows/feature-development.md)** ⭐ Complete feature lifecycle
- **[Project Initialization](workflows/project-initialization.md)** - Project setup
- **[PRD Creation](workflows/prd-creation.md)** - Requirements documents
- **[Sprint Management](workflows/sprint-management.md)** - Agile processes
- **[Deployment](workflows/deployment.md)** - Release procedures

## 🎯 By Role

### For Developers
1. [Task Execution Workflow](development/task-execution-workflow.md)
2. [API Handler Architecture](development/api-handler-architecture-standards.md)
3. [Local CI/CD Validation](development/local-cicd-validation-guide.md)
4. [Feature Development Workflow](workflows/feature-development.md)

### For DevOps/SRE
1. [Helm Chart Standards](development/helm-chart-standards.md)
2. [Deployment Workflow](workflows/deployment.md)
3. [Local CI/CD Validation](development/local-cicd-validation-guide.md)

### For Architects
1. [Database Patterns Guide](development/database-patterns-guide.md)
2. [Architecture Review Workflow](workflows/architecture-review.md)
3. [Design Documentation](design/)

### For Product/QA
1. [PRD Creation Workflow](workflows/prd-creation.md)
2. [Testing Strategy](workflows/testing-strategy.md)
3. [TaskForge Usage Guide](development/taskforge-usage-guide.md)

### For Project Managers
1. [Master Task Management](development/master-task-management.md)
2. [TaskForge Usage Guide](development/taskforge-usage-guide.md)
3. [Sprint Management Workflow](workflows/sprint-management.md)

## 📂 Directory Structure

```
docs/
├── README.md                   # This file - navigation hub
├── development/                # Core standards and guides (30 files)
│   ├── task-execution-workflow.md
│   ├── master-task-management.md
│   ├── taskforge-usage-guide.md
│   ├── api-handler-architecture-standards.md
│   ├── helm-chart-standards.md
│   └── ... (see development/ for full list)
├── workflows/                  # Process workflows
│   ├── feature-development.md
│   ├── project-initialization.md
│   ├── prd-creation.md
│   └── ...
├── design/                     # Architecture and design specs
├── operations/                 # Deployment and operations guides
├── testing/                    # Testing strategies and guides
├── security/                   # Security policies and procedures
├── examples/                   # Configuration examples
└── archive/                    # Historical documentation
```

## 🔍 Finding Documentation

### By Topic

- **Task Management**: `development/master-task-management.md`, `development/taskforge-usage-guide.md`
- **Quality Assurance**: `development/task-execution-workflow.md` (quality gates), `.claude/AGENT_GUIDE.md` (QA agents)
- **API Development**: `development/api-handler-architecture-standards.md`, `development/error-response-standard.md`
- **Database**: `development/database-patterns-guide.md`
- **Deployment**: `development/helm-chart-standards.md`, `workflows/deployment.md`
- **Testing**: `development/local-cicd-validation-guide.md`, `workflows/testing-strategy.md`
- **Frontend**: `development/web-ui-api-synchronization.md`

### By Question

- **"How do I start a new feature?"** → [Feature Development Workflow](workflows/feature-development.md)
- **"What's the development process?"** → [Task Execution Workflow](development/task-execution-workflow.md)
- **"How do I validate before pushing?"** → [Local CI/CD Validation](development/local-cicd-validation-guide.md)
- **"How do I deploy?"** → [Deployment Workflow](workflows/deployment.md)
- **"How do I use agents?"** → [Agent Guide](../.claude/AGENT_GUIDE.md)
- **"What are the API standards?"** → [API Handler Architecture](development/api-handler-architecture-standards.md)
- **"How do I write Helm charts?"** → [Helm Chart Standards](development/helm-chart-standards.md)
- **"How do tasks work?"** → [Master Task Management](development/master-task-management.md)

## 🎓 Learning Path

### Week 1: Foundation
1. Read Root CLAUDE.md
2. Read Agent Guide
3. Read Task Execution Workflow
4. Read Master Task Management
5. Try a small task following the workflow

### Week 2: Standards
1. Read API Handler Architecture Standards
2. Read Helm Chart Standards
3. Read Local CI/CD Validation Guide
4. Practice validation locally

### Week 3: Workflows
1. Read Feature Development Workflow
2. Read Deployment Workflow
3. Complete a feature following workflows
4. Get QA and Devils Advocate review

### Week 4: Mastery
1. Review all skeptic agents
2. Understand quality gates deeply
3. Mentor others on the process
4. Contribute documentation improvements

## 📝 Documentation Standards

### Creating Documentation

1. **Use Markdown**: GitHub-flavored markdown
2. **Clear Structure**: Headings, lists, code blocks
3. **Cross-Reference**: Link to related docs
4. **Examples**: Include practical examples
5. **Update Date**: Add "Last Updated" footer

### Organizing Documentation

- **development/**: Standards, guides, and patterns
- **workflows/**: Step-by-step process documentation
- **design/**: Architecture and design decisions
- **operations/**: Deployment and operational guides
- **examples/**: Configuration examples
- **archive/**: Historical documentation (not current)

### Maintaining Documentation

- Review quarterly for accuracy
- Update when processes change
- Archive obsolete documentation
- Keep examples current
- Test documented procedures

## 🔄 Documentation Updates

This documentation is living and should evolve with the project:

- **Add**: New standards, workflows, or guides
- **Update**: When processes change
- **Archive**: When practices become obsolete
- **Link**: Cross-reference related documentation
- **Example**: Add real-world examples

## 💡 Contributing

When adding documentation:

1. Determine correct directory
2. Use clear, descriptive filename
3. Follow markdown standards
4. Add cross-references
5. Update this README if needed
6. Include examples
7. Test instructions work

## ❓ Getting Help

- **Can't find documentation?** Check this README or search repository
- **Documentation unclear?** Open an issue or ask the team
- **Want to contribute?** Follow contribution guidelines
- **Need process help?** Engage the Prime Orchestrator agent

## 🎯 Remember

**Documentation is code**. It should be:
- Version controlled
- Peer reviewed
- Tested for accuracy
- Updated regularly
- Easy to find and navigate

Good documentation saves hours of confusion and prevents mistakes.

---

**Last Updated**: 2025-10-29
**Maintained By**: Development Team
