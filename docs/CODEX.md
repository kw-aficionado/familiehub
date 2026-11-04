# FamilieHub i Codex

FamilieHub fortsætter som **samme GitHub-repository**: `kw-aficionado/familiehub`.
Der kræves ingen omskrivning, flytning af databasen eller ny produktionsserver.
Codex er udviklingsmiljøet; den valgfrie AI-assistent inde i FamilieHub er en separat funktion.

## Tilføj til dine eksisterende Codex-projekter

### Lokal Codex / desktop

1. Klon repositoryet til en lokal mappe, eller opdatér din eksisterende klon. Lav ikke en ny kopi, hvis du allerede har mappen.
2. Åbn Codex, vælg **Add new project / Åbn projekt** (på macOS også `Cmd+O`), og vælg repositoryets rodmappe — den med `go.mod` og `AGENTS.md`.
3. Vælg et lokalt arbejdsmiljø, og brug projektets almindelige godkendelsesindstillinger. Der er ingen grund til at give ubegrænset adgang.
4. Start med prompten nedenfor. Codex læser `AGENTS.md` som projektinstruktioner; `docs/CODEX.md` indeholder overdragelsen.

```sh
git clone https://github.com/kw-aficionado/familiehub.git
cd familiehub
sh scripts/setup-dev.sh
sh scripts/check.sh
```

Ved en eksisterende klon: kontrollér først `git status` og bevar lokale ændringer; hent derefter opdateringen gennem dit normale Git-flow.
Denne fil registrerer **ikke i sig selv** projektet i en personlig Codex-oversigt, og den importerer ikke samtalehistorik. Den nødvendige projektviden er i stedet gemt her og i `AGENTS.md`.

### Codex Cloud

Opret eller vælg et cloudmiljø med repositoryet **`kw-aficionado/familiehub`**.
Vælg den GitHub-forbindelse, som faktisk har adgang til dette repository; en anden tilsluttet GitHub-konto er ikke automatisk den rigtige.
Installér Go 1.23+, en C-compiler, SQLite-developmentpakke og Node 22+.
Kør `sh scripts/setup-dev.sh` og `sh scripts/check.sh`, og gennemgå testresultaterne før publicering af miljøet.
Der kræves **ingen produktionshemmeligheder**, AI-nøgler eller adgang til familiens private database for normale udviklingsopgaver.

Den aktuelle officielle opsætning findes i [Codex Cloud-guiden](https://learn.chatgpt.com/docs/environments/cloud-environments).
For desktop: [OpenAI's vejledning til at åbne en projektmappe i Codex](https://help.openai.com/en/articles/20001615-prism-to-codex-migration-guide).
[AGENTS.md-dokumentation](https://learn.chatgpt.com/docs/agent-configuration/agents-md).
Menunavne kan variere mellem appversioner. Repo-klargøring, miljøpublicering og åbning af projektet er tre forskellige handlinger.

## Første prompt

> Fortsæt udviklingen af FamilieHub i dette eksisterende repository. Læs AGENTS.md, README.md og docs/CODEX.md. Kontrollér git status, branch og de aktuelle tests, før du ændrer noget. Bevar familiens eksisterende data, iPadOS 15-understøttelsen og Docker/Yggdrasil-opsætningen. Brug kun fiktive data til screenshots. Giv mig en kort status på dansk og arbejd derefter på min næste konkrete opgave. Foretag ikke en omskrivning eller en produktionsdeploy uden en særskilt anmodning.

## Projektkort

| Område | Filer |
| --- | --- |
| HTTP, sessioner, kalender, opgaver og gentagelser | `main.go`, `accounts.go`, `chore_templates.go` |
| Planlægning og startdato | `planner.go`, `planner_start_date.go`, `web/planner-dates.js` |
| Valgfri AI og påmindelser | `ai.go`, `integrations.go`, `email_notifications.go`, `push_notifications.go` |
| Direkte kalenderforbindelser | `direct_calendars.go`, `direct_calendar_fetch.go`, `calendar_owners.go` |
| SQLite og transaktioner | `internal/store/` |
| MCP, OAuth og sikker planflytning | `internal/mcpaccess/`, `mcp_integration.go`, `web/mcp-ui.js` |
| Brugerflade | `web/index.html`, `web/app.js`, `web/style.css`, `web/saver-schedule.js` |
| Regressionstests | Rodens `*_test.go`, `internal/**/*_test.go`, `tests/*.test.js` |
| Udgivelse | `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `Dockerfile`, `rune/` |

Kontrollér de faktiske filer, hvis strukturen ændres; tabellen er ikke en erstatning for kodegennemgang.

## Beslutninger, der skal bevares

- Forsiden starter i **Dag**, ikke Uge. Uge er stadig en frivillig visning.
- **Se alle dagens opgaver** åbner hele listen for den viste dag. Forsiden viser kun fire opgaver som forhåndsvisning; den fulde dagsliste må ikke begrænses til fire.
- **AI Opgaveplanlægger** ligger under **Indstillinger**, lige efter **Opgavearkiv**. Den separate AI-familieassistent bliver på Opgaver.
- Planen har eksplicit startdato, standard i dag i dansk tid, med genvejene I dag/I morgen/Næste mandag. Alle ugedage accepteres.
- Opgaver må ikke være aktive eller generere afkrydsning/påmindelse før planens start. Første korte uge og første mandagsbytte vises særskilt.
- Opgavebyrde er point og minutter, ikke blot antal opgaver. Den oprindelige målgruppe er to teenagere med ugentlig rotation, men planlæggeren understøtter flere medlemmer.
- AI er valgfri. Den deterministiske motor beregner fordelingen; ekstern AI forklarer den. Undgå at beskrive dette som en autonom agent med fri skriveadgang.
- Der vises et forslag, som godkendes før lagring. Efter lagring bliver brugeren i Indstillinger med kvittering og link til startugen.
- Pauseskærmen er normalt kun aktiv kl. **20.00–07.00** efter inaktivitet, med egne indstillinger pr. enhed, separat fra temaet.
- Det eksisterende hus-ikon bruges også som favicon og Apple touch-ikon.

## Status og kendte afgrænsninger

Kildegrundlaget for denne overdragelse var `8d9af8c4` (PR #1–#7 er flettet ind).
Læs altid den aktuelle HEAD og CI-status, ikke kun dette historiske referencepunkt.
Dokumentationsopdateringen tilføjer aktuelle browserbilleder og udviklervejledning, ikke nye ændringer i familiens data.

Den nuværende planner kan oprette en ny plan, men samme plan kan oprettes igen ved en ny godkendelse. Undgå utilsigtede dubletter; generel planner-idempotens er ikke implementeret som en del af dokumentationsopdateringen. MCP-flytning har derimod idempotente kvitteringer.
Planflytning flytter hele uger og afviser planer med afkrydsninger. Det er ikke en vilkårlig historikomskrivning.
Alder, fravær og en fuld samtalebaseret planlægningsdialog blev drøftet som idéer, men skal ikke beskrives som færdige funktioner uden kodeverifikation.
Google/iCloud/Microsoft/HA og fysisk iPadOS-adfærd kræver separate tests med de relevante tjenester/enheder. Demobilleder beviser ikke disse forbindelser.

## Test, billeder og levering

```sh
sh scripts/check.sh
# Valgfrit: rigtige browserbilleder med isolerede, fiktive data
python3 -m venv .venv-screenshots
. .venv-screenshots/bin/activate
python -m pip install -r scripts/screenshots-requirements.txt
python -m playwright install chromium
python scripts/capture-screenshots.py
```

På Linux kan browseren desuden kræve systempakker: se Playwrights officielle installationsvejledning.
`capture-screenshots.py` starter selv den rigtige Go-app på loopback med en ny midlertidig SQLite-database og et tilfældigt login. Scriptet accepterer ikke en eksisterende serveradresse eller produktionsdatabase. Billeder og proveniens gemmes i `docs/screenshots/`.

Brug en feature-branch og PR. Push til `main` udløser en separat Docker-publicering med det eksisterende `0.1.1`-tag. Kontrollér både CI og publicering. Genstart/opdatér først den eksisterende server, når imaget er klar. **Behold `/data`; brug aldrig Wipe som opdateringsmetode.**

Rapportér separat, hvad der er committet, testet, flettet, publiceret og installeret. At have GitHub-adgang er ikke det samme som at have liveadgang til FamilieHub eller at have registreret et projekt i ejerens Codex-app.
