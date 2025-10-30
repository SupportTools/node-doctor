# Project Setup Checklist

Use this checklist to ensure complete setup of your project from the Nexmonyx template.

## ✅ Initial Setup

### Get the Template
- [ ] Use GitHub template button OR clone repository
- [ ] Navigate to project directory
- [ ] Verify all template files present (`ls -la`)

### Run Initialization
- [ ] Make scripts executable: `chmod +x scripts/*.sh`
- [ ] Run initialization: `./scripts/init-from-template.sh`
- [ ] Provide all required variables when prompted
- [ ] Verify substitution completed successfully

### Verify Variable Substitution
- [ ] Check for remaining variables: `grep -r "{{.*}}" . --exclude-dir=.git`
- [ ] Should return zero results
- [ ] If any found, manually update or re-run substitution

## ✅ Git Configuration

### Initialize Repository
- [ ] Initialize git: `git init` (if new repo)
- [ ] Review changes: `git diff`
- [ ] Stage files: `git add .`
- [ ] Create initial commit: `git commit -m "Initialize from Nexmonyx template"`

### Configure Git Hooks
- [ ] Set hooks path: `git config core.hooksPath .githooks`
- [ ] Test pre-push hook: `.githooks/pre-push`
- [ ] Verify hooks working

### Remote Repository (if applicable)
- [ ] Create GitHub repository
- [ ] Add remote: `git remote add origin <URL>`
- [ ] Push to remote: `git push -u origin main`

## ✅ Project Customization

### Core Configuration Files

**makefile**
- [ ] Update PROJECT_NAME variable
- [ ] Update REGISTRY variable
- [ ] Update COMPONENTS list with your services
- [ ] Add build targets for each component
- [ ] Customize deployment targets
- [ ] Test: `make help`

**.validation.json**
- [ ] Update components section with your components
- [ ] Adjust test_timeout values
- [ ] Set coverage_threshold per component
- [ ] Configure validation tools
- [ ] Test: `./scripts/validate-pipeline-local.sh --quick`

**CLAUDE.md** (if needed)
- [ ] Add project-specific instructions
- [ ] Document your technology stack
- [ ] Reference custom workflows
- [ ] Add project context

### Agent Customization

**.claude/agents/specialized/**
- [ ] Review existing specialized agents
- [ ] Add project-specific domain experts
- [ ] Update agent-registry.json
- [ ] Test agent invocation

### Documentation Updates

**README.md**
- [ ] Update project description
- [ ] Add project-specific setup instructions
- [ ] Update features list
- [ ] Add team/contact information

**docs/development/**
- [ ] Review all standards documents
- [ ] Update for project-specific needs
- [ ] Remove non-applicable sections
- [ ] Add custom standards

## ✅ Development Environment

### Prerequisites Check
- [ ] Check all tools: `make check-prerequisites`
- [ ] Install missing tools
- [ ] Verify Go version: `make check-go-version`
- [ ] Verify Docker: `docker --version`
- [ ] Verify kubectl: `kubectl version --client`
- [ ] Verify Helm: `helm version`

### Project Dependencies
- [ ] Initialize Go modules: `go mod init github.com/{{ORG}}/{{PROJECT}}`
- [ ] Download dependencies: `go mod download`
- [ ] Verify mod file: `go mod tidy`

### Build Verification
- [ ] Try building: `make build-local`
- [ ] Fix any build errors
- [ ] Verify all components build
- [ ] Test makefiles: `make help`

## ✅ Validation Setup

### Local Validation
- [ ] Run quick validation: `make validate-quick`
- [ ] Run full validation: `make validate-pipeline-local`
- [ ] Address any validation failures
- [ ] Verify all checks pass

### Pre-Push Validation
- [ ] Setup git hooks (already done above)
- [ ] Test pre-push hook
- [ ] Verify blocking on failures

### Component Validation
- [ ] Create validation scripts for each component (if needed)
- [ ] Add to validate-pipeline-local.sh
- [ ] Test component validation: `make validate-component COMPONENT=api`

## ✅ CI/CD Setup (If Using)

### GitHub Actions
- [ ] Review `.github/workflows/` examples
- [ ] Create pipeline-v2.yml (customize from examples)
- [ ] Configure secrets in GitHub repository settings
- [ ] Set up environment protection rules
- [ ] Test pipeline with first push

### Required Secrets
- [ ] REGISTRY_USERNAME
- [ ] REGISTRY_PASSWORD
- [ ] KUBECONFIG_DEV (if deploying)
- [ ] KUBECONFIG_STG (if deploying)
- [ ] KUBECONFIG_PRD (if deploying)
- [ ] Any API keys or credentials

### Deployment Targets
- [ ] Configure ArgoCD applications (if using)
- [ ] Set up Kubernetes namespaces
- [ ] Configure Helm repository access
- [ ] Test deployment to dev: `make deploy-dev`

## ✅ Quality Gates

### Agent Configuration
- [ ] Review quality gate agents (5 agents)
- [ ] Test QA engineer workflow
- [ ] Test devils-advocate-qa workflow
- [ ] Understand skeptic agents

### Workflow Testing
- [ ] Read task-execution-workflow.md
- [ ] Practice with sample task
- [ ] Complete full workflow cycle
- [ ] Verify all gates working

## ✅ Team Onboarding

### Documentation
- [ ] Team reads docs/README.md
- [ ] Team reviews task-execution-workflow.md
- [ ] Team studies agent guide
- [ ] Team understands quality gates

### Training
- [ ] Demo agent usage
- [ ] Walk through feature workflow
- [ ] Practice with sample task
- [ ] Address questions

### Access Setup
- [ ] Grant repository access
- [ ] Configure KUBECONFIG for team
- [ ] Share registry credentials
- [ ] Set up communication channels

## ✅ Optional Setup

### TaskForge (If Using)
- [ ] Set up TaskForge database
- [ ] Create project in TaskForge
- [ ] Import tasks
- [ ] Configure MCP integration

### Multi-Repository (If Applicable)
- [ ] Set up additional repositories (go-sdk, web-ui, etc.)
- [ ] Configure synchronization workflows
- [ ] Test cross-repo updates
- [ ] Document dependencies

### Monitoring & Observability
- [ ] Set up application monitoring
- [ ] Configure log aggregation
- [ ] Set up alerts
- [ ] Test monitoring pipeline

## ✅ First Feature Development

### Initial Task
- [ ] Select first feature from backlog
- [ ] Create PRD (if needed)
- [ ] Follow task-execution-workflow.md
- [ ] Engage all quality gates
- [ ] Deploy to development

### Validation
- [ ] Code builds successfully
- [ ] All tests pass
- [ ] QA approval received
- [ ] Devils Advocate approval received
- [ ] Successfully deployed

### Review
- [ ] Review workflow effectiveness
- [ ] Adjust gates if needed
- [ ] Update documentation
- [ ] Share learnings with team

## ✅ Maintenance Setup

### Regular Tasks
- [ ] Schedule dependency updates
- [ ] Plan documentation reviews
- [ ] Set up automated security scanning
- [ ] Configure backup procedures

### Continuous Improvement
- [ ] Collect team feedback
- [ ] Iterate on workflows
- [ ] Update standards
- [ ] Share improvements

## 📊 Completion Status

Track your progress:

```
Total Items: 100+
Completed: ___
Remaining: ___
Progress: ___%
```

## 🚨 Critical Items

These items MUST be completed before development:

1. ✅ Variable substitution complete
2. ✅ Git initialized and configured
3. ✅ makefile customized for project
4. ✅ .validation.json configured
5. ✅ Prerequisites installed
6. ✅ Validation pipeline passing
7. ✅ Team has read documentation

## ⚠️ Common Issues

### Issue: Validation Failing
**Solution**: Run `make check-prerequisites` and install missing tools

### Issue: Build Errors
**Solution**: Verify Go version matches requirements, run `go mod tidy`

### Issue: Git Hooks Not Working
**Solution**: Run `cd .githooks && ./setup.sh && cd ..`

### Issue: Variables Not Substituted
**Solution**: Re-run `./scripts/substitute-variables.sh`

## 📚 Reference Documentation

As you complete setup, refer to:

- [TEMPLATE_USAGE.md](TEMPLATE_USAGE.md) - Detailed usage guide
- [VARIABLE_REFERENCE.md](VARIABLE_REFERENCE.md) - All template variables
- [docs/README.md](docs/README.md) - Documentation hub
- [docs/development/task-execution-workflow.md](docs/development/task-execution-workflow.md) - Development process
- [.claude/AGENT_GUIDE.md](.claude/AGENT_GUIDE.md) - Agent usage

## ✅ Final Verification

Before marking setup complete:

```bash
# 1. No template variables remain
grep -r "{{.*}}" . --exclude-dir=.git | wc -l
# Should return: 0

# 2. Validation passes
make validate-pipeline-local
# Should return: Exit code 0

# 3. Help works
make help
# Should show: All commands

# 4. Git hooks active
git config core.hooksPath
# Should return: .githooks

# 5. Build succeeds
make build-local
# Should return: Success (or expected errors for incomplete code)
```

All checks passing? **Setup is complete!** 🎉

## 🎯 Next Steps After Setup

1. Read [Task Execution Workflow](docs/development/task-execution-workflow.md)
2. Select first task from backlog
3. Follow 8-step workflow
4. Engage quality gates
5. Deploy incrementally
6. Iterate and improve

---

**Need help?** See TROUBLESHOOTING.md or review documentation in docs/
