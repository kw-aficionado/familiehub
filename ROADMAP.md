# FamilieHub – næste versioner

## v0.2
- Google Calendar API med OAuth2 (server-side refresh tokens), read/write af aftaler.
- ChoreOps adapter til Home Assistant, mapping af familiemedlemmer/opgaver og en tydelig konfliktstrategi.
- Valg af hvilke HA-kalendere der vises pr. medlem.
- Bedre fejlhåndtering, notifikation når henteansvar flyttes, deduplikeret scheduler.
- Fysisk iPad Air 2-test og målrettede Safari 15-fixes.

## v0.3
- Flere brugere og børneprofiler med forskellige rettigheder.
- Godkendelse af opgaver, rotation og fremtidig ugeplan.
- Lokal kiosk-session med begrænsede rettigheder, separate forældreadgangsoplysninger.
- Sikker hot-backup via SQLite backup API.
- Kompakt Nest Hub visning og Home Assistant Cast test.

## Deployment-acceptkriterier
- `go test ./...` og `go test -race ./...` grønne.
- Reelt GHCR image på både linux/amd64 og linux/arm64.
- Rune importerbar og starter uden håndrettelser på en testinstallation af Yggdrasil.
- HTTPS-subdomæne fungerer; ingen offentlig port-forward til HA.
- Data, sessionsnøgle og pligter overlever genstart.
- Restore fra Yggdrasil backup er testet.
- iPad Air 2 fysisk afprøvet i liggende format.
