# Offene Aufgaben — Go-Port

Stand: nach den Phasen 0–7 auf `feature/go_port`. Diese Liste beschreibt
das, was nach den bisherigen Commits **noch zu tun** ist, damit der Port
produktiv läuft. Jede Aufgabe ist nach Risiko und benötigtem Aufwand
geordnet.

## P0 — Vor dem ersten Container-Build

Diese Punkte blockieren `make build` bzw. `docker build`.

1. **`go mod tidy` lokal ausführen** und das resultierende `go.sum`
   committen. Ohne `go.sum` schlägt sowohl der Nix-Build (`vendorHash`
   wird auf die echten Hashes umgestellt) als auch der Docker-Build (Go
   layer cache + `mod=readonly`) fehl.

2. **`vendorHash` in `flake.nix` setzen.** Erster Lauf mit
   `pkgs.lib.fakeHash`, dann den von Nix gemeldeten SHA in die Datei
   übernehmen.

3. **Compile-Fehler aus dem Sub-Agent-Output bereinigen.** Erwartete
   Klassen (siehe `BUILD.md`):
   - Methoden-Mismatch in `internal/wire/wire.go`. Bereits korrigiert
     für `EnsureNamespace`, `ApplyDockerConfigSecret`, `PatchJSONMerge`
     und `scmmanager.Config`. Weitere Fehler werden beim ersten Build
     sichtbar.
   - Test-Imports in einzelnen Sub-Agent-Test-Files können nicht
     verwendete Pakete listen — `go vet` weist das aus.

## P1 — Funktionale Lücken

4. **`internal/content`: COPY und FOLDER_BASED implementieren.** Aktuell
   nur MIRROR. Stubs mit klaren TODOs in `copy.go` und `folder_based.go`.
   Vorgehen:
   - Quelle in einen TempDir klonen (`git.Service.Clone`).
   - Optional `internal/template.RenderTree` über die Quelle laufen
     lassen, wenn `cfg.Content.Templating == true`.
   - Bei COPY: Files in den GitHandler-Repo kopieren, committen, pushen.
   - Bei FOLDER_BASED: Ordnerstruktur als `namespace/repo`-Pfade
     interpretieren, pro Repo einen GitHandler-Repo anlegen.

5. **OpenShift-UID-Lookup für `features/monitoring`.** Aktuell wird
   `Feature.OpenShiftUID` injiziert; der Runner muss den Wert aus der
   Namespace-Annotation `openshift.io/sa.scc.uid-range` lesen.
   Vorgehen: kleiner Helper `k8s.NamespaceAnnotation(ctx, ns, key)` plus
   eine Setup-Stelle in `internal/wire/wire.go`, die vor `Install`
   läuft.

6. **`runner.Runner.PersistConfig` implementieren.** Soll die gemergte
   Config als Secret (`gop-configuration`, Key `gop-config`) in
   `cfg.Application.GopNamespace` (Fallback: `gop-job`) schreiben.
   Routet auf `k8s.Client.ApplyGenericSecret`.

7. **GitHandler: Repo-Persistenz.** Aktuell legt der Handler ein
   in-process Cache an; ein Crash oder Restart geht den Status verlustig.
   Für den ersten Release reicht das, sollte aber für Idempotenz-Tests
   dokumentiert werden.

8. **ContentSchema Helm-Releases anwenden.** `helm_release.go` enthält
   das Datenmodell, ruft die Strategy aber noch nicht.
   Vorgehen: `deployment.Strategy.Deploy` mit
   `Spec{RepoURL, ChartOrPath, Version, Namespace, ReleaseName, HelmValuesPath, RepoType=RepoHelm}`
   pro `cfg.Content.HelmReleases[i]`.

9. **`Application.storeGopInformationInSecret`-Pendant.** Aktuell als
   TODO im Runner – siehe Punkt 6.

## P2 — Tests + Beobachtbarkeit

10. **End-to-End-Test gegen einen Fake-Cluster.** `internal/k8s`
    enthält bereits Sub-Agent-Tests gegen `client-go`-Fakes. Ein
    `internal/runner/runner_e2e_test.go`, das eine kleine Feature-Liste
    fährt und das Ergebnis verifiziert (drei erstellte Namespaces,
    eine helm-Invocation), fehlt noch.

11. **Tracing-Hook.** Phase 3 hat slog flächendeckend eingeführt. Eine
    `--otel-endpoint`-Flag, die Spans über die Feature-Installation legt,
    wäre für Cluster-Debugging Gold wert.

12. **`helm template` für Diff-Vorschau.** Der Groovy-Code hatte das nie,
    aber `gop --diff` würde via `helm.Client.Template` einen
    Pre-Install-Diff erzeugen.

## P3 — Tooling & Polish

13. **`Makefile`-Targets `docker-build`, `docker-push`.** Trivial; einen
    Tag-Default ableiten aus `git describe`.

14. **`flake.nix`: `dockerToolsBuildImage`-Pfad parametrieren.** Aktuell
    nur `nix build .#oci`. Eine separate `oci-debug`-Variante mit
    `busybox` im PATH wäre für Field-Debugging nützlich.

15. **README im Repo-Root um Hinweis auf Go-Port ergänzen.** Ein kurzer
    Block "Go port lives in `go_src/`" und ein Link zu BUILD.md.

16. **Profile-Equivalenz-Tests.** Für jedes der drei Hauptprofile
    (`minimal`, `content-examples`, `full`) einen Test, der
    `merged := Load(...)` plus `Initialise(merged)` ausführt und das
    Ergebnis als Snapshot festhält. Schützt vor Drift, wenn die
    YAML-Profile bewegt werden.

## P4 — Aufräumen

17. **Groovy-Quelle abkapseln.** Sobald der Go-Port die Funktionalität
    voll abdeckt, in einem Folge-Commit `src/main/groovy` als `legacy/`
    umbenennen und im `Dockerfile`/`pom.xml` als optionalen Pfad
    markieren.

18. **Versionsband-Konsistenz.** `Config.HelmImage`, `K8sVersion` und
    die Konstanten in `Dockerfile`/`flake.nix` driften jetzt
    unabhängig auseinander. Ein kleiner Helper-Test der die drei Werte
    abgleicht, hilft beim nächsten Bump.

## Empfohlene Reihenfolge

P0 → P1.4 (`monitoring` OpenShift) → P1.6 (`PersistConfig`) → P0.3-Pass2
(zweiter Build-Check) → P1.4/8 (ContentLoader) → P2 (Tests).

P3 und P4 können parallel/auf Wunsch des Maintainers laufen.
