package skill

// RegisterBuiltins registers the built-in skills.
// Silently skips any skill name already registered.
func RegisterBuiltins() {
	for _, s := range builtinSkills {
		_ = Register(s)
	}
}

var builtinSkills = []*Skill{
	{
		Name:         "k8s-debug",
		Description:  "Debug Kubernetes pod failures, CrashLoopBackOff, OOMKilled, and networking issues. Use when the user asks to troubleshoot a Kubernetes cluster or application.",
		AllowedTools: []string{"kubectl", "shell_command"},
		Source:       "builtin",
		Instructions: `# Kubernetes Debugging

## Overview
Systematically debug Kubernetes application failures by inspecting pods, events, logs, and resource constraints.

## Workflow

1. **Identify failing pods**
   - Run: kubectl get pods --all-namespaces --field-selector=status.phase!=Running,status.phase!=Succeeded
   - Check for CrashLoopBackOff, ImagePullBackOff, Pending, OOMKilled

2. **Inspect pod details**
   - Run: kubectl describe pod <name> -n <namespace>
   - Check Events section for scheduling failures, resource limits, image pull errors

3. **Check logs**
   - Current: kubectl logs <pod> -n <namespace> --tail=100
   - Previous crash: kubectl logs <pod> -n <namespace> --previous --tail=100
   - All containers: kubectl logs <pod> -n <namespace> --all-containers

4. **Resource analysis**
   - Run: kubectl top pods -n <namespace>
   - Compare actual usage vs requests/limits in pod spec
   - Check node capacity: kubectl describe nodes | grep -A5 "Allocated resources"

5. **Network debugging**
   - Service endpoints: kubectl get endpoints <service> -n <namespace>
   - DNS: kubectl run debug --image=busybox --rm -it -- nslookup <service>
   - Connectivity: kubectl run debug --image=busybox --rm -it -- wget -qO- <service>:<port>

## Common Issues
- **CrashLoopBackOff**: Check previous logs, look for OOM, misconfigured env vars, missing secrets
- **ImagePullBackOff**: Verify image name, registry credentials, network access
- **Pending**: Check node resources, taints/tolerations, PVC binding
- **OOMKilled**: Increase memory limits or optimize application memory usage

## Boundaries
- Do NOT delete or modify production resources without explicit approval
- Always show the user what commands you plan to run before executing`,
	},
	{
		Name:         "incident-response",
		Description:  "Guide security incident response: triage, contain, investigate, and document. Use when the user reports a security incident or asks for incident response help.",
		AllowedTools: []string{"shell_command", "file_system", "@security", "@network"},
		Source:       "builtin",
		Instructions: `# Incident Response

## Overview
Follow a structured incident response process: Identify → Contain → Investigate → Remediate → Document.

## Workflow

### 1. Triage
- Determine severity: P1 (critical/active breach), P2 (confirmed but contained), P3 (suspicious activity), P4 (informational)
- Identify affected systems, services, and data
- Establish timeline: when was it detected, when did it likely start

### 2. Contain
- For active threats: isolate affected systems (network segmentation, disable accounts)
- Preserve evidence: take snapshots, capture logs before rotation
- Do NOT reboot or wipe systems until forensic data is collected

### 3. Investigate
- Collect logs: application logs, access logs, auth logs, system logs
- Check for indicators of compromise (IoCs): unusual IPs, unexpected processes, modified files
- Timeline analysis: correlate events across systems
- Check for lateral movement: other systems accessed from compromised host

### 4. Remediate
- Patch identified vulnerabilities
- Rotate compromised credentials
- Remove unauthorized access
- Update detection rules to catch similar attacks

### 5. Document
- Create incident report with timeline, impact, root cause, actions taken
- Update runbooks and playbooks
- Schedule post-incident review

## Boundaries
- Do NOT make destructive changes without explicit approval
- Preserve chain of custody for forensic evidence
- Escalate immediately if PII or customer data is involved`,
	},
	{
		Name:         "code-audit",
		Description:  "Perform security-focused code audits identifying OWASP Top 10 vulnerabilities, injection flaws, authentication issues, and insecure defaults. Use when the user asks for a security review or code audit.",
		AllowedTools: []string{"code_search", "file_system", "git_repo", "@code"},
		Source:       "builtin",
		Instructions: `# Security Code Audit

## Overview
Systematically review code for security vulnerabilities following OWASP Top 10 and language-specific security best practices.

## Audit Checklist

### Injection (OWASP A03)
- SQL injection: look for string concatenation in queries instead of parameterized statements
- Command injection: check exec/system calls with user input
- XSS: verify output encoding in HTML templates
- LDAP/XPath injection: check query construction

### Authentication (OWASP A07)
- Password storage: must use bcrypt/scrypt/argon2, never MD5/SHA1
- Session management: secure cookies, proper expiration, CSRF protection
- Multi-factor: check if MFA is available for sensitive operations

### Authorization (OWASP A01)
- Access control: verify role checks on all endpoints
- IDOR: check that users can only access their own resources
- Privilege escalation: verify admin functions are properly restricted

### Data Exposure (OWASP A02)
- Sensitive data in logs: check for PII, tokens, passwords in log output
- Error messages: verify stack traces are not exposed to users
- Secrets in code: check for hardcoded API keys, passwords, tokens

### Configuration (OWASP A05)
- Default credentials: check for admin/admin, test accounts
- Debug mode: verify debug/development settings are disabled in production
- CORS: check that origins are properly restricted

## Report Format
For each finding:
1. **Severity**: Critical / High / Medium / Low
2. **Location**: File path and line number
3. **Description**: What the vulnerability is
4. **Impact**: What an attacker could do
5. **Fix**: Specific remediation steps with code example

## Boundaries
- Focus on the highest-impact findings first
- Do NOT modify code unless explicitly asked — this is an audit, not a fix`,
	},
	{
		Name:         "secure-defaults",
		Description:  "Apply language and framework-specific security best practices and secure-by-default coding patterns. Use when writing new code or reviewing for security hygiene.",
		AllowedTools: []string{"code_search", "file_system", "@code"},
		Source:       "builtin",
		Instructions: `# Secure Defaults

## Overview
Apply security best practices when writing or reviewing code. This skill ensures new code follows secure-by-default patterns.

## General Practices

### Input Validation
- Validate all input at system boundaries (API handlers, form processors)
- Use allowlists over blocklists
- Validate type, length, range, and format
- Reject unexpected input rather than trying to sanitize

### Output Encoding
- Encode output based on context (HTML, URL, JavaScript, SQL)
- Use framework-provided encoding functions, not custom implementations
- Apply encoding as late as possible (at render/output time)

### Authentication & Sessions
- Use established libraries (passport, devise, spring-security)
- Hash passwords with bcrypt (cost ≥ 12) or argon2
- Generate session tokens with CSPRNG, ≥ 128 bits entropy
- Set cookie flags: HttpOnly, Secure, SameSite=Lax

### Error Handling
- Never expose stack traces or internal details in production
- Log detailed errors server-side, return generic messages to users
- Use structured error types with error codes

### Cryptography
- Use TLS 1.2+ for all network communication
- Use AES-256-GCM for symmetric encryption
- Use RSA-2048+ or Ed25519 for asymmetric
- Never implement custom crypto — use established libraries

### Dependencies
- Pin dependency versions
- Regularly audit for known vulnerabilities
- Prefer well-maintained libraries with active security response

## Language-Specific

### Go
- Use context.Context for cancellation and timeouts
- Close HTTP response bodies with defer
- Use crypto/rand not math/rand for security
- Validate TLS certificates (don't set InsecureSkipVerify)

### Python
- Use parameterized queries with SQLAlchemy or Django ORM
- Use secrets module for tokens, not random
- Enable CSRF protection in Django/Flask
- Use subprocess with shell=False

### JavaScript/TypeScript
- Use helmet middleware for HTTP headers
- Enable Content-Security-Policy
- Use DOMPurify for HTML sanitization
- Validate JWT signatures before trusting claims

## Boundaries
- These are guidelines, not absolute rules — project context matters
- If a best practice conflicts with project requirements, document why it was bypassed`,
	},
	{
		Name:         "document-manager",
		Description:  "Create, edit, organize, and maintain project documents (plans, reports, RFCs, runbooks, and notes). Use when users ask for document drafting or document updates.",
		AllowedTools: []string{"document_generator", "pdf_generator", "google_docs_manager", "document_preview", "file_system", "text_processor", "json_parser", "diff_generator", "@docs", "@text", "@system"},
		Source:       "builtin",
		Instructions: `# Document Manager

## Overview
Create and maintain high-quality documents with clear structure, version-friendly updates, and actionable content.

## Workflow

1. **Clarify target artifact**
   - Determine document type: RFC, runbook, incident report, implementation plan, meeting notes, changelog.
   - Identify audience and required depth.

2. **Draft with structure first**
   - Start with a concise title and purpose.
   - Use predictable sections (Summary, Context, Decision, Risks, Next Steps).
   - Prefer short bullets over long prose where possible.

3. **Write for maintainability**
   - Keep each section focused on one concern.
   - Include explicit assumptions and open questions.
   - Add timestamps/owners for action items when relevant.

4. **Update existing docs safely**
   - Read current content before editing.
   - Preserve useful context while improving clarity.
   - Make minimal, reviewable edits; avoid unnecessary rewrites.

5. **Final quality pass**
   - Ensure consistent terminology.
   - Remove ambiguity and duplicate statements.
   - Confirm output is immediately usable by the intended reader.

6. **Prepare chat delivery**
   - Use document_preview for generated files.
   - Return a concise preview plus view/download links.

## Boundaries
- Do NOT invent facts; mark unknowns explicitly.
- Do NOT delete important historical context without reason.
- Keep sensitive data out of shared documentation unless explicitly required.`,
	},
	{
		Name:         "research-planner",
		Description:  "Plan research tasks and synthesize findings into decision-ready briefs with sources, assumptions, and next actions.",
		AllowedTools: []string{"web_search", "web_scraper", "http_client", "file_system", "text_processor", "json_parser", "todo_manager", "@network", "@text"},
		Source:       "builtin",
		Instructions: `# Research Planner

## Overview
Run research in a disciplined loop: question framing -> evidence collection -> synthesis -> decision support.

## Workflow

1. **Frame the research question**
   - Define scope, constraints, and success criteria.
   - Break broad topics into 3-6 focused sub-questions.

2. **Collect evidence systematically**
   - Gather multiple sources, prioritize primary/authoritative data.
   - Record source links and key claims.
   - Track uncertain or conflicting points explicitly.

3. **Synthesize into a brief**
   - Distinguish facts vs interpretation.
   - Summarize alternatives with tradeoffs.
   - Highlight confidence level and evidence gaps.

4. **Produce an action plan**
   - Convert findings into clear recommendations.
   - Provide short/medium-term next steps.
   - Include verification checkpoints.

## Output Pattern
- **Question**
- **Key Findings** (bullets)
- **Evidence** (source + claim)
- **Open Risks/Unknowns**
- **Recommended Next Actions**

## Boundaries
- Avoid overconfident conclusions when evidence is thin.
- Prefer transparent uncertainty over fabricated certainty.
- Keep reports concise and decision-focused.`,
	},
	{
		Name:         "pdf-reporting",
		Description:  "Create executive-ready reports and export them to PDF for sharing.",
		AllowedTools: []string{"document_generator", "pdf_generator", "google_docs_manager", "document_preview", "file_system", "text_processor", "json_parser", "@docs", "@text"},
		Source:       "builtin",
		Instructions: `# PDF Reporting

## Overview
Turn raw findings into concise documents and export polished PDF deliverables.

## Workflow

1. Draft structure with document_generator (report or plan template).
2. Fill key sections: summary, findings, recommendations, next actions.
3. Keep sections scannable and decision-focused.
4. Export to PDF using pdf_generator and provide output path.
5. Use document_preview so chat can show a readable preview and links.

## Quality Bar
- Executive summary in first 5-8 lines.
- Explicit risks/unknowns and owners for next actions.
- No sensitive secrets in report body.

## Boundaries
- Do not fabricate findings.
- Prefer concise, factual language over marketing tone.`,
	},
}
