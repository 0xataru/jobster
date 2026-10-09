# jobster

A single-binary job-search aggregator. It fetches remote backend jobs from
public APIs, drops the ones that don't fit, scores the rest against your
criteria, and sends new matches to Telegram.

It runs **once per invocation** and exits, so you schedule it with cron or
GitHub Actions. A SQLite file remembers what it has already sent, so each job
reaches you once.

```
fetch (concurrent) → filter → score → dedupe (SQLite) → Telegram digest
```

## Sources

| Source | How | Config |
|---|---|---|
| [RemoteOK](https://remoteok.com) | JSON API | `sources.remoteok` |
| HN "Ask HN: Who is hiring?" | Algolia HN API, latest monthly thread | `sources.hackernews` |
| [Remotive](https://remotive.com) | JSON API (the free tier returns only a small sample) | `sources.remotive` |
| [We Work Remotely](https://weworkremotely.com) | category RSS feeds | `sources.weworkremotely` |
| [Himalayas](https://himalayas.app) | search API, newest first | `sources.himalayas` |
| [Djinni](https://djinni.co) | RSS per primary keyword | `sources.djinni` |
| Greenhouse boards | `boards-api.greenhouse.io/v1/boards/{slug}/jobs` | `companies[].ats: greenhouse` |
| Ashby boards | `api.ashbyhq.com/posting-api/job-board/{slug}` | `companies[].ats: ashby` |
| Lever boards | `api.lever.co/v0/postings/{slug}` | `companies[].ats: lever` |
| Teamtailor sites | `{slug}.teamtailor.com/jobs.rss` | `companies[].ats: teamtailor` |
| Workable boards | `apply.workable.com/api/v1/widget/accounts/{slug}` | `companies[].ats: workable` |
| Recruitee sites | `{slug}.recruitee.com/api/offers/` | `companies[].ats: recruitee` |
| Personio feeds | `{slug}.jobs.personio.de/xml` | `companies[].ats: personio` |
| Any careers page | detected board, or the page itself | `companies[].careers` |

Aggregators are read through public JSON/RSS endpoints only. HTML is read only
for the careers pages of companies you list yourself (see
[Favorite companies](#favorite-companies)). Some sites can't be supported:

- **golang.cafe, rustjobs.dev**: behind a bot checkpoint.
- **hiddenjobs.dev**: the API is paid-only.
- **jobs.letsgetrusty.com**: HTML only. It is useful for finding company ATS
  slugs to add to `companies`.
- **LinkedIn**: scraping is against its terms.

A failing source is logged and skipped; the run fails only if every source
fails.

## Quick start

```sh
cp config.example.yaml jobster.yaml
go run ./cmd/jobster -dry-run      # print what matches now; no database, no Telegram
go run ./cmd/jobster -dry-run -v   # also log every dropped job and why
```

Requires Go 1.26+. The SQLite driver is pure Go, so `go build ./cmd/jobster`
produces a static binary with no CGO. To install the binary directly:

```sh
go install github.com/0xataru/jobster/cmd/jobster@latest
```

| Flag | Default | |
|---|---|---|
| `-config` | `jobster.yaml` | config file |
| `-dry-run` | off | print all current matches; never touches the database or Telegram |
| `-notify` | `auto` | `auto`: Telegram if both env vars are set, else stdout. `telegram`: fail if they're missing. `stdout`: print only |
| `-v` | off | debug log of every dropped job and its reason |
| `-discover` | off | show how each company's careers page will be read, then exit |

## Telegram setup

1. In Telegram, message [@BotFather](https://t.me/BotFather), send `/newbot`, and copy the **token**.
2. Send your new bot any message (bots can't message you first).
3. Get your **chat id**:
   ```sh
   curl -s "https://api.telegram.org/bot<TOKEN>/getUpdates" | grep -o '"chat":{"id":[0-9-]*'
   ```
4. Try it locally:
   ```sh
   export TELEGRAM_BOT_TOKEN=123456:ABC...
   export TELEGRAM_CHAT_ID=987654321
   go run ./cmd/jobster -notify telegram
   ```

Secrets come only from environment variables. Their names are set by
`telegram.token_env` / `telegram.chat_id_env`, and they never go in the config
file.

## Deploy with GitHub Actions

`.github/workflows/jobster.yml` runs every 6 hours, and on demand from the
Actions tab.

1. Push this repo to GitHub (a private repo is fine).
2. Commit your `jobster.yaml`. It holds no secrets. Without it, the workflow
   uses `config.example.yaml`.
3. Under **Settings → Secrets and variables → Actions**, add
   `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID`.
4. Under **Actions → jobster → Run workflow**, start the first run.

How history persists: `jobster.db` is restored from the Actions cache at the
start of each run and saved at the end. Things to know:

- GitHub evicts caches unused for 7 days, which can't happen at a 6-hour
  schedule. If the cache is ever lost, the next run re-sends up to
  `digest_limit` jobs and then carries on normally.
- GitHub disables scheduled workflows in public repos after 60 days without
  commits. Private repos and manual runs aren't affected.
- The workflow uses `-notify telegram`, so missing secrets fail the run
  instead of silently marking jobs as sent.

To change the schedule, edit `cron` in the workflow; it is in UTC.

### Alternative: run locally with cron

```cron
17 */6 * * * cd /path/to/jobster && ./jobster -notify telegram >> jobster.log 2>&1
```

## Configuration

See [`config.example.yaml`](config.example.yaml) for a complete, commented
example. The defaults target senior, Europe-remote Go/Rust backend roles.

### Match terms

All title, keyword, region and scoring terms use the same syntax:

- **Case-insensitive, whole-word matching.** `go` matches "Go" but not
  "Google" or "ago".
- **A space matches spaces or hyphens.** `us only` also matches "US-only".
- **A hyphen is optional.** `on-site` matches "onsite", "on site" and "on-site".
- **A `=` prefix makes the term case-sensitive.** `"=Go"` matches the language
  but not "go the extra mile". Use it for ambiguous words.

### Filters (`filter`)

A job is dropped if it fails any of these:

| Key | Rule |
|---|---|
| `max_age`, `max_age_by_source` | Posting age limit (`7d`, `36h`, …). `max_age_by_source` overrides it per source kind (`greenhouse`, `ashby`, `lever`, `teamtailor`, `hn`, `remoteok`). ATS boards date jobs by first publication, so they need a longer window. |
| `titles.include` / `titles.exclude` | The title must match one include term and no exclude term. |
| `keywords.required` | Title, description or tags must match at least one. |
| `keywords.exclude` | Title, location or description must match none, e.g. "US only". |
| `regions.allow` / `regions.deny` | Location must match an allow term and no deny term. Ashby, Lever and Teamtailor add "Remote" / "Hybrid" / "On-site" to the location, so `deny: [hybrid, on-site]` works for them. |
| `regions.allow_unspecified` | Keep jobs with no location. This is common on RemoteOK and always the case on Djinni, whose feed has no location: filter Djinni with `sources.djinni.params` instead. |

### Scoring (`scoring`)

| Key | Points |
|---|---|
| `title` | Term → points when found in the title. Negative values are penalties. |
| `text` | Term → points when found in the description or tags. |
| `salary_above` / `salary_below` | Points when a known salary is ≥ / < `min_salary`. Unknown salaries are neutral. Amounts are not converted, so only currencies in `salary_currencies` (default USD, EUR, GBP, CHF) count; others are neutral. |
| `target_company` | Points for any company in `companies`, from any source. |
| `threshold` | Minimum score to be notified. |

Each term counts once. The digest shows the score; run `-dry-run` to see each
job's breakdown (e.g. `+5 title:rust, +1 kubernetes, -2 php`).

### Favorite companies

List the companies you'd like to work for under `companies`. Their jobs get
the `target_company` bonus wherever they appear, and their own open roles are
fetched and filtered like everything else. Three ways to list a company:

```yaml
companies:
  - { name: Supabase, ats: ashby, slug: supabase }    # known board, read via its API
  - { name: Xata, careers: "https://xata.io/careers" } # anything else: start from the careers page
  - { name: Elastic }                                  # bonus only, nothing fetched
```

**With `ats` + `slug`**, the board's public API is read directly. The slug is
the board name in its URL: `boards.greenhouse.io/<slug>`,
`jobs.ashbyhq.com/<slug>`, `jobs.lever.co/<slug>`, `<slug>.teamtailor.com`,
`apply.workable.com/<slug>`, `<slug>.recruitee.com`, `<slug>.jobs.personio.de`.
A wrong slug shows up as a `source failed … 404` warning.

**With `careers`**, jobster loads the page and:

1. If it links to one of those job boards, or is a Recruitee site on the
   company's own domain, it uses that board's API.
2. Otherwise, or if that API fails, it reads the page itself: schema.org
   `JobPosting` data when the page has it, else the links to individual job
   pages (up to 40, below the careers path or under `/jobs/`, `/positions/`,
   …), taking each page's title and text. If it picks the wrong links, set
   `link_pattern`, a regexp matched against the link's URL path:
   `link_pattern: "^/careers/\\d+$"`.

Jobs read from a page have no location or date unless the page publishes
`JobPosting` data, so they pass the region filter as unspecified. Pages that
build their job list with JavaScript have no links to read; they fail with a
"no job links" warning, and you can set `ats`/`slug` instead.

Run `jobster -discover` to see what each careers page resolves to:

```
Xata                     page with 3 job links
Supabase                 ashby:supabase board detected; to skip detection: { ats: ashby, slug: supabase }
Channable                recruitee:jobs.channable.com board detected; keep the careers URL to reach it
```

## Deduplication

Jobs are keyed by normalized company + title, so the same role on RemoteOK and
on the company's own board is reported once. Jobs without a company (Djinni
hides it) are keyed by URL instead. They are also matched by URL, so
a retitled posting isn't reported again. A job is marked sent only after
Telegram accepts the message, so a failed send is retried on the next run.
Jobs not seen for `dedupe_window` (default 90 days) are forgotten, so a role
re-posted later is reported again.

## Development

```sh
go test -race ./...
```

Parsers are tested against saved fixture responses in
`internal/fetch/testdata`, and scoring against the example config.

```
cmd/jobster/      CLI wiring
internal/config   YAML config and defaults
internal/fetch    sources, worker pool (github.com/0xataru/workerpool), HTTP client
internal/filter   hard filters
internal/match    keyword matching
internal/score    scoring
internal/store    SQLite history
internal/notify   Telegram and stdout digests
internal/job      the Job type and dedupe key
```

To add a source, implement `fetch.Fetcher` (`Name`, `Fetch`) with a
`Parse…(io.Reader)` function, add a fixture and a test, then register it in
`buildFetchers` in `cmd/jobster/main.go`.
