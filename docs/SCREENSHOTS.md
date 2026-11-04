# Aktuelle skærmbilleder

Billederne i `docs/screenshots/` optages af `scripts/capture-screenshots.py` med Playwright/Chromium.
Det er **den rigtige HTML/CSS/JavaScript og Go-backend**, ikke en tegnet mockup.
Alle personer, aftaler og opgaver er fiktive og oprettes i en ny midlertidig SQLite-database.
Der kopieres aldrig data fra en eksisterende familieinstallation.

## Genoptag

Forudsætninger: Go/CGO, en C-compiler, SQLite-developmentpakke og Python 3.10+.
Kør fra repositoryets rod:

```sh
python3 -m venv .venv-screenshots
. .venv-screenshots/bin/activate
python -m pip install -r scripts/screenshots-requirements.txt
python -m playwright install chromium
python scripts/capture-screenshots.py
```

På en ren Linux-maskine kan Playwrights browser også kræve systempakker. Brug om nødvendigt `python -m playwright install --with-deps chromium` i et miljø, hvor systeminstallation er godkendt.

Scriptet bygger en midlertidig appbinær, starter den på `127.0.0.1` med en tilfældig port, en ny datamappe og et tilfældigt administratorlogin. Det accepterer ikke en eksisterende serveradresse eller `DATA_DIR`. Det rydder selv den midlertidige proces og datamappe op.
Home Assistant er eksplicit deaktiveret, og der oprettes ingen AI-, SMTP-, push- eller eksterne kalenderforbindelser.

## Motiver

| Fil | Motiv |
| --- | --- |
| `kalender.webp` | Dag som standard, aftaler, fire-kortsforhåndsvisning og genvej til hele dagslisten. |
| `opgaver.webp` | Hele dagens tjekliste, inklusive en udført opgave. |
| `indstillinger.webp` | Arkiv filtreret på `bord` med AI Opgaveplanlægger umiddelbart under arkivet. |
| `planlaegger.webp` | Den åbne planlægger med startdato og datogenveje. |
| `planforslag.webp` | Det faktisk beregnede planforslag i dialogen. |
| `skaermindstillinger.webp` | Pauseskærmens tidsrum og inaktivitet, separat fra temaet. |
| `nattilstand.webp` | Manuel afprøvning af pauseskærmen. |
| `iphone.webp` | Dagsoverblik i en 390-pixel bred mobilviewport. |
| `iphone-sondag.webp` | Ugekalender på mobil rullet til søndag. |

## Verifikation og begrænsninger

Optagelsen kontrollerer rigtigt login, Dag som standard, flere end fire dagsopgaver,
placeringen under Opgavearkiv, valgt startdato, succesfuldt preview-kald,
20.00/07.00-indstillingerne, manuel pauseskærm, syv ugedage og fravær af JavaScript-fejl.
Den gemmer et manifest med dato, browser, kilders SHA-256 og billeders SHA-256/størrelse.

Læs `docs/screenshots/manifest.json` for optagelsesdato og kildegrundlag.
Dagens dato og de tilfældige demo-id'er ændres mellem optagelser; processen er reproducerbar, men billederne er ikke garanteret byte-identiske.
Sammenlign manifestets kildehashes med de relevante ændrede filer, når du vurderer, om billeder skal genoptages.

Screenshots er ikke en fuld ende-til-ende-test af alle funktioner. Særligt kræver fysisk iPad/Safari,
ægte kalender-OAuth, mail, Web Push, AI-udbydere og ekstern MCP separate tests.
Footeren viser `Build demo` for ikke at forveksle demoen med familiens produktionsserver.
Se også de officielle [Playwright-screenshot-](https://playwright.dev/python/docs/screenshots)
og [browserinstallationsvejledninger](https://playwright.dev/python/docs/browsers).
