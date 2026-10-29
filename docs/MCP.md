# MCP-adgang til FamilieHub

MCP giver en eksternt godkendt ChatGPT-forbindelse adgang til familiens opgaver.
Det er en anden forbindelse end den AI-udbyder, der er valgt inde i FamilieHub.
Din OpenAI-/Claude-API-nøgle skal ikke deles med ChatGPT eller indtastes igen.

## Aktivering

1. Brug en offentlig HTTPS-adresse, som videresender til din eksisterende
   FamilieHub-server. En privat IP-adresse (også en privat Tailscale-adresse)
   er ikke i sig selv tilgængelig for en direkte forbindelse fra ChatGPT.
   Brug din eksisterende sikre reverse proxy; udsat ikke port 8080 ukrypteret.
2. Sæt `APP_URL` i Yggdrasil til den samme HTTPS-origin, uden understi,
   og genstart den eksisterende server. Bevar `/data`; brug ikke Wipe.
3. Log ind som administrator. Åbn **Indstillinger -> MCP-adgang til ChatGPT**.
4. Angiv HTTPS-adressen, aktiver MCP og eventuelt **Tillad rettelser efter
   forhåndsvisning**. Gem. MCP-serveradressen vises som `https://dit-domaene/mcp`.
5. I ChatGPT: **Plugins -> + -> Add custom MCP server**. Angiv serveradressen,
   vælg **OAuth**, og brug automatisk klientregistrering (**DCR**).
   Klient-ID og klienthemmelighed skal være tomme. Serveren registrerer en
   offentlig klient med PKCE; ingen delt klienthemmelighed bruges.
6. Godkend på FamilieHubs egen loginside. Kun administratoren kan godkende
   forbindelsen. Skriveadgang kræver også et aktivt tilvalg på samtykkesiden.
7. Installer/aktiver forbindelsen i den samtale, hvor opgaverne skal bruges.
   En kodeopdatering i FamilieHub alene forbinder ikke en eksisterende chat.

De præcise menunavne kan variere efter ChatGPT-konto og workspace-rettigheder.
Adgangskoden indtastes kun på FamilieHubs egen HTTPS-side, aldrig i chatten.
Din adresse skal være tilgængelig for ChatGPT, inklusive OAuth-opdagelse,
registrering, tokenudveksling og `/mcp`; et ekstra proxy-login foran disse
endpoints vil forhindre forbindelsen. FamilieHubs normale login og MCP's
OAuth-beskyttelse maa ikke slås fra for at omgå dette.

## Værktøjer og rettigheder

- `get_family`: dagens dato i Europe/Copenhagen og familiemedlemmers ID/navn/farve.
- `list_plans`, `list_tasks`, `get_week`: eksisterende planer, opgaver og den
  faktiske ugefordeling med roterede ansvarlige. Lister er paginerede.
- `preview_move_plan`: vis en flytning af en eksisterende plan til mandag i
  indeværende eller en kommende uge. Opgave-ID'er og ugedage bevares.
- `preview_update_tasks`: vis konkrete rettelser af titel/ansvarlig; dato og
  gentagelse kan også rettes for enkeltopgaver uden plan/rotation.
- `apply_changes`: anvend netop det godkendte forslag med samme `preview_id`.
  Afhænger af baade OAuth-scope `tasks:write` og appens aktive skrivetilladelse.

Der er ingen MCP-værktøjer til at slette eller oprette opgaver, administrere
brugere, læse eksterne kalendere, hente API-nøgler eller koere vilkårlig SQL.
Det begrænser risikoen for dubletter og utilsigtet adgang til andre data.

## Start en eksisterende plan i denne uge

Bed assistenten om at læse `get_family` og `list_plans`, vise de berørte planer
og lave `preview_move_plan` til den aktuelle mandag. Vælg kun de plan-ID'er, der
faktisk skal flyttes. Godkend derefter det viste forslag.

Planen flyttes uden at genskabe opgaver. Er det fx torsdag, flyttes en torsdags-
eller fredagsopgave frem til denne uge. Opgaver på allerede passerede ugedage
starter først på naeste tilsvarende ugedag; de bliver ikke oprettet som
forsinkede opgaver fra mandag-onsdag. Hele-uge-opgaver kan gaelde den aktuelle
uge. Rotationens faelles startdato følger den nye planstart.

Planer med allerede afkrydsede opgaver afvises af masseflytning. Historiske
registrerede afkrydsninger slettes eller omskrives aldrig via MCP. Hvis en opgave
bliver rettet eller afkrydset efter forhåndsvisningen, afvises hele ændringen,
og der skal laves et nyt forslag. Et forslag udløber efter 10 minutter og ved
skift af kalenderdato. Gentagelse af samme godkendte `preview_id` er idempotent.

## Sikkerhed og tilbagekaldelse

MCP er deaktiveret som standard. Der bruges OAuth authorization-code med PKCE
S256, eksakte registrerede ChatGPT-returadresser, enkeltbrugs-koder og
autentifikation på hvert MCP-kald. Kun HTTPS-originen i indstillingerne bruges
som issuer/resource; Host og X-Forwarded-Host er ikke en autoritetskilde.
Access- og refresh-tokens lagres kun som hashes. Access-tokens gælder 15
minutter; en forbindelse gælder højst 30 dage. Refresh-tokens roterer, og genbrug
af et gammelt refresh-token tilbagekalder hele forbindelsen.

Under MCP-indstillinger vises godkendte forbindelser og en log over anvendte
rettelser. **Tilbagekald adgang** virker straks. Deaktivering af MCP eller skift
af offentlig adresse tilbagekalder alle forbindelser. Skrivetilladelsen kan
slukkes separat. Adgangen inkluderer aldrig API-nøgler, SMTP-adgangskoder,
push-adresser, eksterne kalenderindstillinger eller hele databasen.

MCP understøtter stateless Streamable HTTP med protokolversionerne
2025-03-26, 2025-06-18 og 2025-11-25. GET/SSE-streams og sessions-ID'er er ikke
nødvendige for disse værktøjer. Ukendte/nyere versioner skal forhandles ned.
OAuth-opdagelse:

- `/.well-known/oauth-protected-resource/mcp`
- `/.well-known/oauth-authorization-server`

DCR tillader kun de officielle ChatGPT-callbacks (stabil callback og
callback-ID-varianten). Denne første version er ikke beregnet til vilkårlige
MCP-klienter eller deres localhost-callbacks.

## Verifikation

```sh
go test ./...
go test -race ./...
go vet ./...
node --check web/app.js
node --check web/mcp.js
# Valgfri browserkontrol med syntetiske data:
python -m pip install playwright==1.57.0
python -m playwright install chromium
go build -o /tmp/familiehub-mcp-test .
python tests/mcp_browser_test.py --binary /tmp/familiehub-mcp-test
```

Browserkontrollen routes til en lokal, midlertidig testserver. Ingen data sendes
til eksterne tjenester, og testen ændrer ikke familiens rigtige database.

Referencer: OpenAI's dokumentation for custom MCP servers og OAuth samt MCP
2025-11-25 Streamable HTTP/Authorization:

- https://developers.openai.com/api/docs/guides/custom-mcp-server
- https://developers.openai.com/plugins/build/auth
- https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
