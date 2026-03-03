# Collaboration Runbook

Reference guide for Jira, Confluence, PagerDuty, and JSM integration patterns.

---

## Jira (via MCP or CLI)

### Issue Management

```bash
# Search issues (JQL via curl)
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  "$JIRA_URL/rest/api/3/search?jql=project=$PROJECT+AND+status+NOT+IN+(Done,Closed)+ORDER+BY+priority+DESC&maxResults=20" | \
  jq '.issues[] | {key, summary: .fields.summary, status: .fields.status.name, priority: .fields.priority.name, assignee: .fields.assignee.displayName}'

# Create an issue
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  -X POST "$JIRA_URL/rest/api/3/issue" \
  -H "Content-Type: application/json" \
  -d '{
    "fields": {
      "project": {"key": "'$PROJECT'"},
      "summary": "Issue title",
      "description": {"type": "doc", "version": 1, "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Description"}]}]},
      "issuetype": {"name": "Bug"},
      "priority": {"name": "High"}
    }
  }'

# Get issue details
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  "$JIRA_URL/rest/api/3/issue/$ISSUE_KEY" | \
  jq '{key, summary: .fields.summary, status: .fields.status.name, description: .fields.description}'
```

### Sprint Management

```bash
# Get active sprints
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  "$JIRA_URL/rest/agile/1.0/board/$BOARD_ID/sprint?state=active" | \
  jq '.values[] | {id, name, startDate, endDate}'

# Issues in current sprint
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  "$JIRA_URL/rest/agile/1.0/sprint/$SPRINT_ID/issue" | \
  jq '.issues[] | {key, summary: .fields.summary, status: .fields.status.name, assignee: .fields.assignee.displayName}'
```

---

## Confluence (via MCP or API)

### Page Operations

```bash
# Search pages
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  "$CONFLUENCE_URL/rest/api/content/search?cql=text~\"<search-term>\"+AND+type=page&limit=10" | \
  jq '.results[] | {id, title, url: ._links.webui}'

# Get page content
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  "$CONFLUENCE_URL/rest/api/content/$PAGE_ID?expand=body.storage" | \
  jq '{title, body: .body.storage.value}'
```

---

## PagerDuty

### Incident Management

```bash
# List active incidents
curl -s -H "Authorization: Token token=$PD_API_KEY" \
  "https://api.pagerduty.com/incidents?statuses[]=triggered&statuses[]=acknowledged" | \
  jq '.incidents[] | {id, title: .title, status: .status, urgency: .urgency, service: .service.summary, created: .created_at}'

# Get incident details
curl -s -H "Authorization: Token token=$PD_API_KEY" \
  "https://api.pagerduty.com/incidents/$INCIDENT_ID" | \
  jq '.incident | {title, status, urgency, service: .service.summary, created_at, assignments: [.assignments[].assignee.summary]}'

# List on-call schedules
curl -s -H "Authorization: Token token=$PD_API_KEY" \
  "https://api.pagerduty.com/oncalls?earliest=true" | \
  jq '.oncalls[] | {user: .user.summary, schedule: .schedule.summary, escalation: .escalation_policy.summary}'

# Acknowledge an incident
curl -s -H "Authorization: Token token=$PD_API_KEY" \
  -H "Content-Type: application/json" \
  -X PUT "https://api.pagerduty.com/incidents/$INCIDENT_ID" \
  -d '{"incident": {"type": "incident_reference", "status": "acknowledged"}}'
```

### Service Management

```bash
# List services
curl -s -H "Authorization: Token token=$PD_API_KEY" \
  "https://api.pagerduty.com/services" | \
  jq '.services[] | {id, name, status, escalation_policy: .escalation_policy.summary}'
```

---

## JSM (Jira Service Management)

### Service Request Management

```bash
# Get request types for a service desk
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  "$JIRA_URL/rest/servicedeskapi/servicedesk/$SD_ID/requesttype" | \
  jq '.values[] | {id, name, description}'

# Get open requests
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  "$JIRA_URL/rest/servicedeskapi/servicedesk/$SD_ID/queue/$QUEUE_ID/issue" | \
  jq '.values[] | {key, summary: .fields.summary, status: .fields.status.name}'

# SLA information
curl -s -u "$JIRA_EMAIL:$JIRA_API_TOKEN" \
  "$JIRA_URL/rest/servicedeskapi/request/$ISSUE_KEY/sla" | \
  jq '.values[] | {name, remainingTime: .ongoingCycle.remainingTime.friendly}'
```

---

## Best Practices

- **Link incidents to Jira** — always create or reference a tracking ticket
- **Update status pages** — keep stakeholders informed during incidents
- **Time-box investigations** — escalate if not resolved within defined SLAs
- **Post-mortem** — create a Confluence page documenting root cause and prevention
