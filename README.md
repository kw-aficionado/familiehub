# FamilieHub v0.1.1 – familiekalender som Yggdrasil Rune

En selvhostet familietavle til iPad Air 2, nyere tablets og telefoner.

**Status:** Kørbar MVP. Go + SQLite + indlejret HTML/CSS/JavaScript (ingen npm-afhængigheder). Designet med browserkompatibilitet til iPadOS 15. Docker-imaget bygges automatisk via GitHub Actions ved push til `main`. Runen kræver, at dette workflow er gennemført, og at GHCR-pakken er offentligt tilgængelig (eller Yggdrasil har registry-adgang).

## Funktioner i v0.1.1

- Dag- og ugevisning, frem/tilbage, swipe på touchskærm og "I dag".
- Opret, redigér og slet lokale aftaler, inkl. legeaftaler og afhentningsansvar.
- Familiemedlemmer med farvekoder; filtrering efter person.
- Opret opgaver til én dag, én hel uge, hver uge eller hver anden uge. Opgaver som gælder en hel uge afkrydses én gang for hele ugen (mandag–søndag).
- Home Assistant REST-integration til læsning af konfigurerede kalender-entities.
- Push-notifikationstest via HA `notify`-service og automatiske afhentningspåmindelser 30 min. før en lokal legeaftale slutter.
- Adgangskode, sessionscookies, CSRF-beskyttelse, rate limit på login, sikkerhedsheaders.
- SQLite-database og sessionsnøgle i `/data`.
- Server-Sent Events mellem åbne FamilieHub-skærme; fallback refresh hver 60. sekund.

**Begrænsninger:**

- Google Kalender/iCloud skal i denne version være tilknyttet **Home Assistant** som `calendar.*`-entities. De vises kun i læsetilstand. Lokale FamilieHub-aftaler synkroniseres **ikke** tilbage til Google/iCloud.
- Opgaver ligger foreløbig i FamilieHubs SQLite, ikke i ChoreOps. ChoreOps-adapteren er næste fase.
- Der er ét delt FamilieHub-login for husstanden (ikke individuelle adgangsroller endnu).
- Ingen komplet Google Nest Hub-oplevelse; kompakt castskærm er planlagt.
- Ingen PWA-offline-cache: appen kræver online forbindelse til backend.
- Browserlayout er implementeret, men bør afprøves fysisk på den gamle iPad inden permanent opsætning.

## Opgaver for hele uger og hver anden uge

I **Opgaver → + Opgave → Gentagelse** kan du nu vælge:

- **Én hel uge**: vis opgaven fra mandag til søndag i den uge, du vælger under startdato, og afkryds kun én gang.
- **Hver uge (hele ugen)**: en ny opgave hver mandag, som kan løses én gang inden søndag.
- **Hver anden uge (hele ugen)**: samme som ovenfor, men kun hver anden uge. Første aktive uge er ugen med den valgte startdato.
- **Hver anden uge (én dag)**: gentages hver 14. dag på samme ugedag som startdatoen.
- Tidligere **Hver uge (én dag)** og **Hver dag** er uændrede.

Ved afkrydsning af en hel uge vises opgaven som udført på alle syv dage. Næste aktive uge starter med en frisk afkrydsning. Ugeopgaver giver en samlet påmindelse kl. 16 om søndagen, hvis de ikke er færdige. Eksisterende data bevares; SQLite-skemaet skal ikke migreres.

**Opdatering uden ny Rune:** Dette udviklingsspor genbruger bevidst Docker-tagget `0.1.1` fra den eksisterende Rune. Yggdrasil trækker den senest publicerede version af dette tag på **Restart**, når GitHub Actions er grønt. Det er et bevidst mutable tag til løbende opdateringer; for reproducerbare udgivelser anbefales et fast versions-tag senere.


## Opgavearkiv og overblik (ny)

- Brug tandhjulet **Indstillinger** øverst til højre for en separat indstillingsside med fanerne **Opgavearkiv** og **Alle opgaver**.
- **Opgavearkiv**: opret, redigér og slet skabeloner med opgavenavn, standardansvarlig og gentagelse. En skabelon er ikke en aktiv opgave. Skabeloner lagres i SQLite under `/data`.
- **Alle opgaver**: samlet søgbar liste over aktive/oprettede opgaver, også dem der ikke gælder den valgte kalenderdag. Redigér eller slet herfra.
- På forsiden: vælg **+ Opgave → Fra opgavearkivet**, og en skabelon udfylder navn, ansvarlig og gentagelse. Du vælger stadig selv startdatoen. Knappen **Gem i arkiv** kan gemme en ny skabelon direkte fra en opgave.
- Kalenderens kort er gjort lavere; pilen for eksterne kalenderaftaler vises ved siden af tidspunktet.
- Migrering er additiv (`CREATE TABLE IF NOT EXISTS task_templates`), så eksisterende opgaver, afkrydsninger og aftaler bevares.
- Runen og GHCR-tagget `0.1.1` er uændrede; efter et **grønt build** kræves kun genstart af Yggdrasil-serveren. Kontroller build-nummeret nederst på forsiden.

## 1. Byg lokalt (Linux)

Forudsætninger: Go 1.23+, gcc og SQLite3 development headers.

```bash
go test ./...
go build -o familiehub .
mkdir -p data
APP_PASSWORD='VAELG-ET-SIKKERT-PASSWORD-PA-MINDST-12-TEGN' \
 APP_URL='http://localhost:8080' DATA_DIR='./data' \
 ./familiehub -listen 127.0.0.1:8080
```

Åbn `http://localhost:8080`. Data gemmes i `./data`.

## 2. Byg Docker-imaget

Denne app er en enkelt Docker-container, ikke en Docker Compose stack.

```bash
docker build -t ghcr.io/kw-aficionado/familiehub:0.1.1 .
docker run --rm -p 8080:8080 \
  -e APP_PASSWORD='VAELG-ET-SIKKERT-PASSWORD-PA-MINDST-12-TEGN' \
  -e APP_URL='http://localhost:8080' \
  -v familiehub-data:/data \
  ghcr.io/kw-aficionado/familiehub:0.1.1
```

Bygningen er opsat til både AMD64 og ARM64; buildx/GitHub Actions kan bygge begge.

## 3. GitHub / GHCR-publicering

Repository: https://github.com/kw-aficionado/familiehub

1. GitHub Actions-workflowet `.github/workflows/release.yml` bygges automatisk ved push til `main` og på semver-tags.
2. Kontrollér bygningen under **Actions** i repositoryet. Et grønt workflow er nødvendigt, før Yggdrasil kan hente imaget.
3. Image-adressen for første version er `ghcr.io/kw-aficionado/familiehub:0.1.1`.
4. Gør GHCR-pakken **public**, eller tilføj registry-credentials under Yggdrasils **Settings → Container registries**. Et offentligt repository gør ikke automatisk alle GHCR-pakker offentlige.
5. I Yggdrasil: **Runes → Browse GitHub** og angiv repository `kw-aficionado/familiehub`, folder `rune`, branch `main`. Du kan også uploade Rune-YAML direkte.

Opdateringer: En ny version bør få et nyt semver-tag og en Rune med opdateret `docker.image`-tag og `version`-felt. `0.1.0` fra `main` fungerer som første offentliggørelse; undgå at bruge det tag som en permanent rullende kanal.

## 4. Installér som Yggdrasil Rune

Rune: `rune/familiehub.yaml`

1. I Yggdrasil Panel: **Runes → Carve a rune (upload)**. Upload YAML-filen.
2. Opret en server fra **FamilieHub**-Runen.
3. Angiv en stærk `APP_PASSWORD` (min. 12 tegn).
4. Hvis du ønsker HA-integration: udfyld `HA_URL`, `HA_TOKEN` og `HA_CALENDARS`.
5. Slå serverens **Auto-forward fra**, hvis du eksponerer via NPM eller Cloudflare Tunnel.
6. Sæt serverens subdomæne til f.eks. `familie` og aktivér HTTPS i Yggdrasil-domæneintegrationen.
7. Start serveren. Yggdrasil publicerer containerens interne port 8080 på en automatisk valgt hostport.
8. Aktivér **Autostart** på serveren i Yggdrasil, så den starter efter en genstart.
9. Åbn subdomænet, log ind, og opret familiemedlemmer, opgaver og aftaler.

Runens `docker.data_path` er `/data`. Yggdrasil monterer her serverens persistente dataområde. På Yggdrasil overstyrer panelen typisk image-USER til sin servicekonto; dette passer, fordi den også ejer serverens dataområde.

## Opgradering fra 0.1.0

1. Kontrollér at GitHub Actions har publiceret GHCR `0.1.1` med status success, og at container-imaget kan hentes fra Yggdrasil.
2. Under Yggdrasil → Runes: vælg `kw-aficionado/familiehub`, `rune`, `main` og importér Runen igen (`version: 2`).
3. Genstart den **eksisterende FamilieHub-server** i Yggdrasil, så den benytter det nye Docker-image. Opret ikke en ny server og brug ikke Wipe; behold `/data`.
4. Åbn FamilieHub → Integrationer (ikonet med sky) for at se API-status og kalendertest separat.

**Fejlretninger i 0.1.1:** Home Assistant kalendere returnerer start/slut som objekter med `dateTime` eller `date` (heldag). Begge understøttes nu, og det tidligere strengformat accepteres fortsat. Integrationer viser specifikke og sanitiserede fejl (401, 403, DNS, timeout, TLS og ugyldig kalender). Personformularen forklarer forskellen mellem en `device_tracker` og et HA `notify.mobile_app_*`-kald.

## 5. Forbind eksisterende Home Assistant

Opret en separat HA-bruger til integrationen. Generér et long-lived access token og udfyld Rune-variablerne:

| Variabel | Eksempel | Forklaring |
|---|---|---|
| `HA_URL` | `http://192.168.1.20:8123` | LAN- eller VPN-adresse fra *Yggdrasil-serveren* til HA |
| `HA_TOKEN` | `...` | Secret, kun i backend |
| `HA_CALENDARS` | `calendar.familie,calendar.skole` | HA-kalendere, som dashboardet skal læse |
| `APP_URL` | `https://familie.example.dk` | Brug offentlig HTTPS-adresse |

Tilføj Google Kalender eller CalDAV/iCloud til HA separat. Kalender-entities kan herefter vises i FamilieHub, hvis HA's REST calendar API er aktiveret og brugerens rettigheder tillader adgang.

For personlige notifikationer:

1. Installér Home Assistant Companion App på modtagerens telefon.
2. Find telefonens notify-service i HA, f.eks. `notify.mobile_app_mors_iphone`.
3. I FamilieHub: **Familiemedlemmer → Redigér**. Skriv **kun** service-delen, f.eks. `mobile_app_mors_iphone`.
4. Brug **Test push**.
5. Lokale legeaftaler med valgt `Hvem henter?` udløser notifikation **30 minutter før aftalens sluttid**. Appen sender desuden en samlet påmindelse om uafsluttede opgaver kl. 16:00. Scheduleren forsøger afsendelse i et 10-minutters vindue efter påmindelsestidspunktet.

**Sikkerhed:** Hvis Yggdrasil er en ekstern VPS, brug en privat VPN-forbindelse til HA. Udsæt ikke HA's port 8123 direkte mod internettet. APP_PASSWORD er fælles for husstanden, så del den kun med familien. Den offentlige FamilieHub skal altid have HTTPS.

## 6. Backup og restore

SQLite-filen og sessionsnøglen er under `/data` (`familiehub.sqlite` og `session.key`).

Rune-backup bruger `include: ["."]`, så hele dataområdet kommer med. For en **konsistent backup** af SQLite anbefales at stoppe FamilieHub-serveren i Yggdrasil før en backup og genstarte bagefter. Ved restore bør serveren også være stoppet. Løbende hot-backup bør senere udføres via SQLite Backup API for at undgå inkonsistent kopiering under skrivninger.

## 7. Kør verificering

```bash
go test ./...
go test -race ./...
node --check web/app.js
```

Lokal `go test` dækker login, CSRF, CRUD, gentagelseslogik, HA-kalenderdata, pushkald og persistens. Docker-build, Yggdrasil-installasjon, fysisk Safari på iPadOS 15 og ægte Google/HA-forbindelser er endnu **ikke** end-to-end-verificeret.

## Referencer

- [Yggdrasil Rune Schema](https://github.com/kristianwind/yggdrasil/blob/main/docs/reference/rune-schema.md)
- [Yggdrasil Runes](https://github.com/kristianwind/yggdrasil/blob/main/docs/guides/runes.md)
- [Yggdrasil Networking](https://github.com/kristianwind/yggdrasil/blob/main/docs/guides/networking.md)
- [Yggdrasil Runes with an LLM](https://github.com/kristianwind/yggdrasil/blob/main/docs/guides/runes-with-an-llm.md)

## Farvekoder og tags i Google Kalender

Fra oktober 2026 kan den eksisterende Home Assistant kalenderintegration farvelægge aftaler med hashtags i **titlen**. Eksempler:

- `#Anton #Sport Badminton` viser **Badminton** med Antons personfarve og en diskret sportsetiket.
- `#Alma #Skole Terminsprøve` viser **Terminsprøve** med Almas farve.
- `#Anton #Alma Biograf` får en todelt farvestribe og vises under begge personfiltre.
- `#Sport Fodboldkamp` får sportens kategorifarve, når der ikke er person-tags.

Person-tags matcher eksisterende familiemedlemmer (uden forskel på store/små bogstaver).
Et navn med mellemrum kan bruges samlet (`Anne Marie` → `#AnneMarie`).
Understøttede kategorier: `#Sport`, `#Skole`, `#Arbejde`, `#Hjemme`, `#Familie`,
`#Fritid`, `#Privat` og `#Legeaftale`. Ukendte hashtags bevares i titlen.
Aftaler uden tags bevarer den neutrale farve. Tags ændrer **aldrig** noget i Google Kalender.
Google Kalenders egne eventfarver kan ikke læses via den nuværende Home Assistant REST-integration.

Farver hentes fra personprofiler i FamilieHub. Tjek at personnavnet svarer til hashtagget.
De nye metadata bruges også i personfiltre. For at filtrere på Anton trykkes på `Anton` øverst.

### Undgå cachede opdateringer

Appens HTML/JS/CSS sendes med `Cache-Control: no-store`. Desuden kan `/api/version`
bruges til at se `commit` og version (også uden login). Build-hashen vises i footeren,
så det kan verificeres, at Yggdrasil startede det nyeste Docker-image.
Ved genstart henter Yggdrasil normalt imaget igen. Hvis hash ikke ændres: kontrollér
Yggdrasils serverlog og hvilken Docker-image-tag der er knyttet til serverens Rune.
Runen er ikke ændret; GHCR-tagget `0.1.1` bliver genudgivet ved push til `main`.

## iPad Air 2 (portraet)

Header og kalendernavigation har mindre lodret padding. Kalenderkortene bruger igen 12 px for titler og 11 px for klokkeslet, saa skriften ikke skaleres ned for at rumme alle syv dage. I 701-900 px bredde viser ugevisningen hoejst to linjer af hvert aftalenavn; tryk paa aftalen for at se hele titlen. Alle syv ugedage beholdes i samme overblik, og brugerskabte opgaver bevares uforandret.

## Navigation og opgaver (ny hovedside)

- Tryk på FamilieHub-logoet eller titlen for at komme til #kalender. Tre hovedfaner: #kalender, #opgaver og #indstillinger; browserens tilbageknap fungerer.
- Kalenderforsiden viser højst fire opgaver, med udestående først. **Se alle →** åbner den fulde opgaveside.
- Opgavesiden har visningerne I dag, Denne uge og Alle opgaver, personfilter samt statusfilter. Ugevisningen bruger det autentificerede API `GET /api/tasks/week?start=YYYY-MM-DD` til at vise korrekt gentagelse og afkrydsning.
- Hele-uge-opgaver vises som ét kort pr. aktiv uge, mens daglige opgaver vises pr. dag. Opgaver kan afkrydses direkte.
- Samme farver som i kalenderen bruges på opgavekortene og personnavnene. Fællesopgaver får neutral farve.
- Opgavearkivet, familiemedlemmer og HA-integration er samlet under Indstillinger.
- Ingen databasemigrering eller ændring af Rune-filen er nødvendig.
