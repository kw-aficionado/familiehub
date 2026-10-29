# MCP-adgang og flytning af opgaveplaner

Funktionen bruger FamilieHubs eksisterende administratorlogin og SQLite-database.
Ekstern adgang er **slaaet fra som standard**. En opdatering flytter ikke opgaver
eller giver automatisk en ekstern assistent adgang.

## Flyt en plan i FamilieHub

Aabn **Indstillinger -> MCP-adgang og flyt opgaveplan** som administrator.
Vaelg den eksisterende plan og den nye startuges mandag. Klik **Vis datoflytning**,
gennemgaa datoer/advarsler og godkend. Funktionen virker ogsaa uden aktiveret MCP.

Alle opgaveserier i den valgte plan og deres rotationsstart flyttes samlet.
Opgave-id, titel, ansvarlig og gentagelse bevares. Der oprettes ingen kopier.
Planer med afkrydsninger afvises, saa historikken ikke omskrives. Aendringer siden
forhaandsvisningen afviser godkendelsen. Gentagne godkendelser af samme forslag
returnerer samme kvittering og flytter ikke opgaverne igen.

En flytning til den aktuelle uges mandag kan give forfaldne opgaver fra tidligere
ugedage og aktivere eksisterende paamindelser. Advarslen vises foer godkendelse.
Det er flytning af en **hel ugeplan**, ikke en automatisk "start i dag"-funktion.

## Forbind en MCP-klient

1. Brug en HTTPS-adresse til FamilieHub, som MCP-klienten kan naa. En privat LAN-
   eller VPN-adresse er ikke i sig selv tilgaengelig fra en cloudklient. Angiv
   adressens origin uden sti under MCP-indstillinger, og aktiver MCP.
2. Opret en OAuth-klient i FamilieHub med den **praecise callback-adresse**, som
   klientens opsaetning viser. FamilieHub bruger manuelt registrerede klienter;
   der annonceres ingen dynamisk registrering.
3. Indtast MCP-endpointet (din origin + `/mcp`), OAuth client ID og client secret
   i klientens forbindelsesopsatning. Hemmeligheden vises kun ved oprettelsen og
   skal aldrig deles i en chat, et screenshot eller GitHub.
4. Gennemfoer login og godkendelse paa FamilieHubs egen side. Administratoren
   kan til enhver tid tilbagekalde klienten eller deaktivere MCP.

OAuth bruger authorization code med PKCE S256, eksakt redirect-match,
ressourcebundne tokens, roterende refresh tokens og hoejst 30 dages autorisation.
Understoettede scope: `family:read`, `plans:write`, `offline_access`.

Read-only giver fire vaerktoejer til familieoverblik, arkiv og planer.
`plans:write` tilfoejer `familyhub_preview_move_plan` og
`familyhub_apply_move_plan`. Assistenten skal vise forslaget og indhente udtrykkelig
godkendelse foer apply. Ingen adgang til kalenderindhold, brugerkonti, API-noegler,
SMTP, vilkaarlig SQL eller fuld administration.

## Drift og test

MCP-ruter: `/mcp`, `/mcp/authorize`, `/mcp/oauth/token`, `/mcp/oauth/revoke`.
Metadata: `/.well-known/oauth-authorization-server` og
`/.well-known/oauth-protected-resource/mcp`. En reverse proxy skal videresende
Authorization-header og disse stier. FamilieHub er stateless Streamable HTTP;
GET-streams er ikke understottet. Administrative sider bruger eksisterende
sessionscookie og CSRF; en sessionscookie giver aldrig MCP API-adgang.

Migreringerne er additive. Tag normal backup af `/data` foer opdatering, behold
samme datavolumen, og brug ikke Wipe. Adgange og aendringslog lagres i SQLite.

```sh
go test ./...
go test -race ./...
for file in web/*.js; do node --check "$file"; done
```

Testene omfatter OAuth, scope, tilbagekaldelse, udloeb, replay, godkendelse,
forhaandsvisning, atomisk rollback, samtidige kald, tidszone og appens eksisterende
auth/CSRF. En rigtig ChatGPT-forbindelse kraever stadig installation, HTTPS og
brugerens godkendelse; tests er ikke bevis paa liveadgang til en familieinstallation.
