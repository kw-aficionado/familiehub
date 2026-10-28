# FamilieHub v0.1.1 – familiekalender som Yggdrasil Rune

En selvhostet familietavle til iPad Air 2, nyere tablets og telefoner.

**Status:** Kørbar MVP. Go + SQLite + indlejret HTML/CSS/JavaScript (ingen npm-afhængigheder). Designet med browserkompatibilitet til iPadOS 15. Docker-imaget bygges automatisk via GitHub Actions ved push til `main`. Runen kræver, at dette workflow er gennemført, og at GHCR-pakken er offentligt tilgængelig (eller Yggdrasil har registry-adgang).

## Funktioner i v0.1.1

- Dag- og ugevisning, frem/tilbage, swipe på touchskærm og "I dag".
- Opret, redigér og slet lokale aftaler, inkl. legeaftaler og afhentningsansvar.
- Familiemedlemmer med farvekoder; filtrering efter person.
- Opret pligter, daglig/ugentlig gentagelse, afkrydsning per dato.
- Home Assistant REST-integration til læsning af konfigurerede kalender-entities.
- Push-notifikationstest via HA `notify`-service og automatiske afhentningspåmindelser 30 min. før en lokal legeaftale slutter.
- Adgangskode, sessionscookies, CSRF-beskyttelse, rate limit på login, sikkerhedsheaders.
- SQLite-database og sessionsnøgle i `/data`.
- Server-Sent Events mellem åbne FamilieHub-skærme; fallback refresh hver 60. sekund.

**Begrænsninger:**

- Google Kalender/iCloud skal i denne version være tilknyttet **Home Assistant** som `calendar.*`-entities. De vises kun i læsetilstand. Lokale FamilieHub-aftaler synkroniseres **ikke** tilbage til Google/iCloud.
- Pligter ligger foreløbig i FamilieHubs SQLite, ikke i ChoreOps. ChoreOps-adapteren er næste fase.
- Der er ét delt FamilieHub-login for husstanden (ikke individuelle adgangsroller endnu).
- Ingen komplet Google Nest Hub-oplevelse; kompakt castskærm er planlagt.
- Ingen PWA-offline-cache: appen kræver online forbindelse til backend.
- Browserlayout er implementeret, men bør afprøves fysisk på den gamle iPad inden permanent opsætning.

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
5. Lokale legeaftaler med valgt `Hvem henter?` udløser notifikation **30 minutter før aftalens sluttid**. Appen sender desuden en samlet påmindelse om uafsluttede pligter kl. 16:00. Scheduleren forsøger afsendelse i et 10-minutters vindue efter påmindelsestidspunktet.

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
