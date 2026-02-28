# Web Research Intelligence Agent

> **Audience:** Business Analysts, Sales Teams, Competitive Intelligence, Due Diligence  
> **Scenario:** Given a website URL or company name, Genie autonomously discovers and synthesizes crucial information — company overview, products, technology stack, team, funding, market position, and more — by leveraging web search, HTTP crawling, browser automation, and GitHub exploration.

---

## The Problem

When evaluating a company, a potential partner, a competitor, or a prospect, gathering comprehensive intelligence requires:

1. **Scattered information** — key facts are spread across the company's website, press releases, social media, GitHub, Crunchbase, LinkedIn, news articles, and more
2. **Time-consuming research** — manually visiting dozens of pages and cross-referencing data takes hours
3. **Missed insights** — important details (tech stack, open-source activity, hiring signals, funding rounds) are easy to overlook
4. **Stale knowledge** — information changes frequently; a one-time research effort quickly becomes outdated
5. **Unstructured output** — findings end up in scattered notes rather than a clean, actionable report

This agent automates the entire research workflow: it takes a company name or URL, fans out across multiple data sources in parallel, and produces a structured intelligence report.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                    CodeOwner (Orchestrator)                       │
│  Persona: Research analyst with web + GitHub access              │
│  Memory: Tracks findings across all research sub-tasks           │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────┐  ┌───────────────┐  ┌───────────────────────┐ │
│  │  Sub-Agent 1  │  │  Sub-Agent 2   │  │  Sub-Agent 3          │ │
│  │  Web Researcher│  │ Tech Profiler │  │  Market Analyst       │ │
│  │  (parallel)   │  │  (parallel)    │  │  (parallel)           │ │
│  └──────┬───────┘  └──────┬────────┘  └──────┬────────────────┘ │
│         │                  │                   │                  │
│  ┌──────┴──────────────────┴───────────────────┴──────────────┐  │
│  │                   Shared Tool Registry                      │  │
│  │  • http_request (crawl company website pages)              │  │
│  │  • web_search (discover info across the internet)          │  │
│  │  • playwright (browser automation for JS-heavy sites)      │  │
│  │  • MCP: github (explore open-source repos & activity)      │  │
│  │  • memory_store / memory_search (track findings)           │  │
│  │  • send_message (deliver final report)                     │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Configuration

### `genie.toml`

```toml
skills_roots = ["../skills"]

[mcp]
[[mcp.servers]]
name = "playwright"
transport = "stdio"
command = "npx"
args = ["-y", "playwright-mcp"]

[[mcp.servers]]
name = "github"
transport = "stdio"
command = "npx"
args = ["-y", "@anthropic/github-mcp-server"]
env = { GITHUB_TOKEN = "${GITHUB_TOKEN}" }
include_tools = [
  "search_repositories",
  "get_file_contents",
  "list_commits",
  "search_code",
  "list_branches",
  "get_repository",
]
```

---

## Tool Usage Guidelines

### Preferred Tools

| Task | Tool | Why |
|------|------|-----|
| Search the web for information | `web_search` | Broad discovery of company info, news, funding, reviews |
| Fetch specific web pages | `http_request` | Fast, returns text content for known URLs |
| Browse JS-heavy / interactive sites | `playwright` | Handles SPAs, login walls, dynamic content |
| Explore GitHub presence | MCP `github` tools | Search repos, read READMEs, check activity |
| Store interim findings | `memory_store` | Sub-agents store results, orchestrator retrieves |
| Retrieve stored findings | `memory_search` | Orchestrator pulls sub-agent results for synthesis |
| Deliver report | `send_message` | Single consolidated report to user |

### Anti-Patterns

- ❌ **Never use browser tools for simple page fetches** — `http_request` is faster and cheaper for static content
- ❌ **Never give `send_message` to sub-agents** — they will spam the user with duplicate messages
- ❌ **Never search for the same query twice** — cache results in `memory_store`
- ❌ **Never scrape content behind paywalls or login walls** without user authorization

---

## Interaction Flow

### Step 1 — User requests company research

```
You: Research the company "Acme Corp" (https://acmecorp.example.com).
     Find out: what they do, their products, tech stack, team, funding,
     competitors, and any notable news.
```

### Step 2 — Orchestrator plans the research

The orchestrator creates a research plan based on the target:

```
Genie: I'll research Acme Corp across multiple dimensions. Here's my plan:

       🎯 Target: Acme Corp (https://acmecorp.example.com)

       I'll run 3 parallel sub-agents:
       1. Web Researcher — crawl the website + search for company info, news, funding
       2. Tech Profiler — explore GitHub repos, identify tech stack, open-source activity
       3. Market Analyst — find competitors, market position, customer reviews, partnerships
```

### Step 3 — Multi-agent execution

The orchestrator delegates using `create_agent`:

```
# Sub-Agent 1: Web Researcher
create_agent(
  goal="Research 'Acme Corp' at https://acmecorp.example.com.

        Phase 1 — Website Crawl:
        Use http_request to fetch key pages:
        - Homepage (/)
        - About page (/about, /company, /about-us)
        - Products/Services page (/products, /solutions, /services, /platform)
        - Pricing page (/pricing)
        - Blog (/blog — latest 3-5 posts)
        - Careers page (/careers, /jobs)
        - Contact page (/contact)

        If pages return errors or are JS-heavy, note them for browser follow-up.

        Phase 2 — Web Search:
        Use web_search to find:
        - '[Company] funding rounds'
        - '[Company] founders team leadership'
        - '[Company] news latest'
        - '[Company] reviews ratings'
        - '[Company] partnerships integrations'

        Extract and store:
        1. Company description and mission
        2. Products and key features
        3. Pricing model (if public)
        4. Team / leadership bios
        5. Funding history (investors, amounts, rounds)
        6. Recent news and press mentions
        7. Job openings (signals growth areas)

        Store structured findings using memory_store with key 'web_research'.
        Do NOT call send_message.",
  task_type="tool_calling",
  tool_names=["http_request", "web_search", "memory_store"]
)

# Sub-Agent 2: Tech Profiler
create_agent(
  goal="Profile the technology footprint of 'Acme Corp'.

        Phase 1 — GitHub Exploration:
        Using GitHub MCP tools:
        1. Search for repositories owned by the company (try org names:
           'acmecorp', 'acme-corp', 'acme')
        2. For each repo found:
           - Read the README for project descriptions
           - Check primary languages used
           - Note star count, recent commit activity, contributor count
           - Look at recent commits for development velocity
        3. Search for code mentioning the company name in other repos
           (community adoption signals)

        Phase 2 — Tech Stack Detection:
        Using http_request on the company website:
        1. Check HTTP response headers for server technology
        2. Look for meta tags, script references, framework indicators
        3. Check for common SaaS integrations (analytics, chat widgets, etc.)

        Using web_search:
        1. Search '[Company] tech stack' or '[Company] engineering blog'
        2. Search for the company on StackShare, BuiltWith, or similar

        Store findings using memory_store with key 'tech_profile'.
        Do NOT call send_message.",
  task_type="tool_calling",
  tool_names=["http_request", "web_search", "memory_store"],
  mcp_server_names=["github"]
)

# Sub-Agent 3: Market Analyst
create_agent(
  goal="Analyze the market position of 'Acme Corp'.

        Using web_search:
        1. Search '[Company] competitors' and '[Company] alternatives'
        2. Search '[Company] market share' or '[Company] industry report'
        3. Search '[Company] customer reviews' on G2, Capterra, TrustRadius
        4. Search '[Company] partnerships' and '[Company] integrations'
        5. Search for the company on Crunchbase, PitchBook, or LinkedIn

        For each competitor found:
        - Brief description of what they do
        - How they compare to the target company

        Extract and store:
        1. Direct competitors (with brief descriptions)
        2. Market category / industry vertical
        3. Customer sentiment (from reviews)
        4. Key partnerships and integrations
        5. Estimated company size / employee count
        6. Social media presence and engagement

        Store findings using memory_store with key 'market_analysis'.
        Do NOT call send_message.",
  task_type="tool_calling",
  tool_names=["http_request", "web_search", "memory_store"]
)
```

### Step 4 — Report synthesis

The orchestrator retrieves all findings and generates a consolidated report:

```
Genie: ## 🏢 Company Intelligence Report: Acme Corp

       ### Company Overview
       | Field               | Details                                      |
       |---------------------|----------------------------------------------|
       | Name                | Acme Corp                                    |
       | Website             | https://acmecorp.example.com                 |
       | Founded             | 2019                                         |
       | Headquarters        | San Francisco, CA                            |
       | Employees           | ~150 (estimated from LinkedIn)               |
       | Industry            | Developer Tools / Cloud Infrastructure       |
       | Mission             | "Making cloud deployment effortless"         |

       ### Products & Services
       - **Acme Platform** — Cloud deployment automation with one-click infra
       - **Acme CLI** — Command-line tool for managing deployments
       - **Acme Enterprise** — Self-hosted version with SSO, audit logs

       Pricing: Freemium model. Free tier for small teams, paid plans
       starting at $49/mo per seat.

       ### Technology Stack
       | Layer        | Technologies                                    |
       |--------------|-------------------------------------------------|
       | Backend      | Go, gRPC, PostgreSQL                            |
       | Frontend     | React, TypeScript, Next.js                      |
       | Infra        | Kubernetes, Terraform, AWS                      |
       | Open Source   | 12 public repos, 2.3k total stars              |

       Most active repo: `acmecorp/acme-cli` (890 stars, 45 contributors,
       last commit 2 days ago)

       ### Funding & Financials
       | Round   | Date     | Amount  | Lead Investor        |
       |---------|----------|---------|----------------------|
       | Seed    | Mar 2019 | $3M     | First Round Capital  |
       | Series A| Nov 2020 | $15M    | Sequoia              |
       | Series B| Jun 2022 | $50M    | a16z                 |

       Total raised: $68M

       ### Team & Leadership
       - **Jane Doe** — CEO & Co-founder (ex-Google, Stanford CS)
       - **John Smith** — CTO & Co-founder (ex-AWS, contrib to Terraform)
       - **Sarah Lee** — VP Engineering (ex-Stripe)

       Currently hiring: 12 open roles (heavy on backend engineers
       and enterprise sales → signals enterprise push)

       ### Competitive Landscape
       | Competitor      | Overlap    | Differentiation                   |
       |-----------------|------------|-----------------------------------|
       | Vercel          | High       | Acme is infra-agnostic            |
       | Render          | Medium     | Acme targets enterprise           |
       | Railway         | Medium     | Acme has stronger CLI tooling     |

       ### Recent News
       - 📰 "Acme Corp launches enterprise tier" — TechCrunch (Jan 2024)
       - 📰 "Acme reaches 10,000 developers" — Company Blog (Dec 2023)
       - 📰 "Acme partners with Datadog" — press release (Nov 2023)

       ### Customer Sentiment
       - G2 Rating: 4.6/5 (120 reviews)
       - Strengths: ease of use, CLI experience, documentation
       - Weaknesses: limited monitoring, pricing at scale

       ---
       📊 Research confidence: High (multiple corroborating sources)
       📅 Report generated: 2024-01-15
```

---

## Key Concepts Demonstrated

| Concept | How It's Used |
|---------|---------------|
| **CodeOwner orchestrator** | Plans the research, coordinates sub-agents, synthesizes the report |
| **Multi-agent (ReAcTree)** | 3 parallel sub-agents: web research, tech profiling, market analysis |
| **MCP tools** | GitHub MCP server for `search_repositories`, `get_file_contents`, etc. |
| **http_request** | Fast page fetching for company websites without browser overhead |
| **web_search** | Broad internet search for company info, news, funding, reviews |
| **playwright** | Browser automation for dynamic/JS-heavy pages when needed |
| **Working memory** | Sub-agents store findings via `memory_store`; orchestrator retrieves with `memory_search` |

---

## Research Categories

### 1. Company Overview
- Mission statement and description from website
- Founding date, location, and company size
- Key leadership and founders

### 2. Products & Services
- Product names, descriptions, and key features
- Pricing model and tiers (if public)
- Target audience and use cases

### 3. Technology Stack
- GitHub repos → languages, frameworks, dependencies
- Website technology → HTTP headers, meta tags, scripts
- Engineering blog posts → self-reported stack
- StackShare / BuiltWith data

### 4. Funding & Financials
- Funding rounds → Crunchbase, press releases, news articles
- Investors and board members
- Revenue signals (pricing × estimated customers)

### 5. Market Position
- Direct competitors and alternatives
- Market category and industry vertical
- Customer reviews (G2, Capterra, TrustRadius)
- Analyst reports and industry coverage

### 6. Team & Culture
- Leadership bios from website and LinkedIn
- Current job openings → growth signals and priorities
- Glassdoor / team size trends
- Conference talks, blog posts, community engagement

### 7. Technology & Open Source
- GitHub organization activity (repos, stars, contributors)
- Open-source projects maintained by the company
- Developer community engagement
- API documentation and developer experience

### 8. News & Signals
- Recent press coverage and announcements
- Partnership and integration announcements
- Product launches and feature releases
- Regulatory or legal developments

---

## Prompts — What to Say

### Full Company Research

```
Research the company behind https://example.com.
Find out everything you can: what they do, products, tech stack,
team, funding, competitors, and recent news. Generate a full report.
```

### Quick Company Overview

```
Give me a quick overview of "Acme Corp" — what do they do,
who founded it, and how big are they?
```

### Competitive Analysis

```
Research "Acme Corp" and identify their top 5 competitors.
For each competitor, explain how they differ and where they overlap.
```

### Tech Stack Deep Dive

```
Analyze the technology stack of https://example.com.
Check their GitHub repos, website headers, and any engineering blog posts.
What languages, frameworks, and infrastructure do they use?
```

### Funding & Growth Research

```
Research the funding history of "Acme Corp".
Find all funding rounds, investors, and any revenue/growth signals.
```

### Pre-Meeting Prep

```
I have a meeting with "Acme Corp" tomorrow.
Research them and give me a one-page briefing: what they do,
recent news, key people I might meet, and talking points.
```

### Open Source Activity

```
Analyze the open-source presence of "Acme Corp".
Find their GitHub org, list their public repos, check activity levels,
and identify their most impactful projects.
```

---

## Getting Started

```bash
# 1. Clone and navigate to the example
cd examples/docs-qa-validator

# 2. Set environment variables
export GEMINI_API_KEY="..."
export GITHUB_TOKEN="..."        # For exploring company GitHub repos

# 3. Start local embeddings (optional but recommended)
ollama serve &
ollama pull nomic-embed-text

# 4. Run Genie
genie grant

# 5. Ask Genie to research a company:
#    "Research the company behind https://example.com. Generate a full intelligence report."
```

---

## Why Multi-Agent?

**Without multi-agent:** Researching a company across website, GitHub, news, reviews, and funding sources sequentially could take 20-30 minutes. Each dimension requires multiple searches and page fetches.

**With multi-agent:** 3 sub-agents work in parallel — one crawls the website and searches for general info, one profiles the tech stack and GitHub presence, and one analyzes market position and competitors. The orchestrator only handles planning and synthesis. Estimated: ~3-5 minutes for a comprehensive report.

The sub-agents use `task_type="tool_calling"` which routes to faster, cheaper models — ideal for the repetitive web searches, HTTP requests, and GitHub API calls that don't require deep reasoning.
