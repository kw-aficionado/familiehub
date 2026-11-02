# Pauseskærm med tidsrum

Under **Indstillinger -> Skærm og nattilstand** kan pauseskærmen konfigureres
uafhængigt af lyst/mørkt tema.

Standard: **kun kl. 20.00-07.00, dansk tid**, efter **10 minutters inaktivitet**.
Der vises aldrig automatisk pauseskærm før tidsrummets start. Hvis enheden
allerede har været inaktiv længe nok, kan skærmen starte ved kl. 20.00.
Automatisk pauseskærm lukkes igen ved slutningen af tidsrummet uden et tryk.

- Vælg **Kun i det valgte tidsrum**, og ret **Aktiv fra** / **Aktiv indtil**.
- Vælg **Hele døgnet** for kun at bruge inaktivitetstiden.
- Vælg **Aldrig** under **Pauseskærm efter inaktivitet** for at slå den fra.
- **Afprøv pauseskærmen** er en manuel visning, og virker også uden for tidsrummet.

Indstillinger gemmes automatisk på den enkelte enhed. Eksisterende tema,
nattetider og inaktivitetstid bevares. En tidligere indstilling til Aldrig
forbliver slået fra efter opdateringen. Enheder uden tidsrum får 20.00-07.00.
Sommertid/vintertid beregnes i Europe/Copenhagen uanset enhedens tidszone.
To ens klokkeslæt afvises; brug Hele døgnet i stedet.

Skjulte browserfaner stopper automatisk aktivering. Når fanen åbnes igen,
starter en ny inaktivitetsperiode. Login og åbne dialoger dækkes ikke.
Hvis browserens lokale lagring er blokeret, vises det ved indstillingerne.
Dette styrer FamilieHubs pauseskærm, ikke iPad'ens systemlås eller lysstyrke.

## Test

`node --test tests/*.test.js` omfatter grænser, midnat, sommertid, tilpasning,
lagring, migrering, inaktivitet, manuel afprøvning, skjulte faner og dialoger.
Testene udfører de faktiske frontend-funktioner med et kontrolleret ur og DOM.
