# Offene Aufgaben — Go-Port

Stand: nach iteration 2 (Phase 11) auf `feature/go_port`. `make check`,
`golangci-lint run`, `nix flake check` und das `bin/gop`-Smoketest-Set
laufen grün — siehe `TEST_PLAN.md`. Diese Liste beschreibt das, was
**noch zu tun** ist, damit der Port produktiv läuft. Jede Aufgabe ist
nach Risiko und benötigtem Aufwand geordnet.

## P0 — Vor dem ersten Container-Build

Diese Punkte blockieren `make build` bzw. `docker build`.

1. ~~`go mod tidy` lokal ausführen und `go.sum` committen.~~
   → done in phase 9 / iteration 1 (TEST_PLAN.md), `go.sum` ist
   committed.

2. ~~`vendorHash` in `flake.nix` setzen.~~ → done in iteration 2:
   `sha256-Fo708koTJeD1sXxqYYeDq/K6cGDtCOfEMaSZp3zUP6g=`, getestet via
   `nix build .#default` und `nix flake check`.

3. ~~Compile-Fehler aus dem Sub-Agent-Output bereinigen.~~
   → done in phase 9 / iteration 1: nur zwei winzige Mismatches im
   Jenkins-Test (siehe TEST_PLAN.md F-1 / F-2). Build, vet, race-test
   und `bin/gop --help` laufen alle clean.

~~T-2 (P0.new): SHA256-Werte verifizieren.~~ → done in phase 12:
alle vier SHA256 (helm-amd64, helm-arm64, kubectl-amd64, kubectl-arm64)
matchen die Upstream-Werte unter
`https://get.helm.sh/helm-v4.1.4-linux-<arch>.tar.gz.sha256sum` bzw.
`https://dl.k8s.io/release/v1.35.4/bin/linux/<arch>/kubectl.sha256`.
Dockerfile-Kommentar trägt das Verifikationsdatum.

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

**T-1 (P2.new)**: Benchmarks für `internal/k8s` (`go test -bench=.`).
Der Adapter ist mit 1,5 s schon der teuerste in der Suite und enthält
die tiefsten Poll-/Wait-Schleifen — der natürliche Ort für
Regression-Detection auf Timings.


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

~~T-3 (P3.new): `apps.default` ohne `meta`-Attribut.~~
→ done in phase 12: `apps.default = flake-utils.lib.mkApp { drv = gop;
inherit (gop) meta; }`.

15. ~~README im Repo-Root um Hinweis auf Go-Port ergänzen.~~
    → done in phase 8: Top-Level README komplett neu, Groovy-README nach
    `retired/README.md` verschoben.

16. **Profile-Equivalenz-Tests.** Für jedes der drei Hauptprofile
    (`minimal`, `content-examples`, `full`) einen Test, der
    `merged := Load(...)` plus `Initialise(merged)` ausführt und das
    Ergebnis als Snapshot festhält. Schützt vor Drift, wenn die
    YAML-Profile bewegt werden.

## P4 — Aufräumen

17. ~~Groovy-Quelle abkapseln.~~ → done in phase 8: alle Groovy-, Maven-
    und Skript-Pfade liegen jetzt unter `retired/`. Das alte
    `Dockerfile`/`pom.xml` ist dort, der Maven-Build über
    `cd retired && ./mvnw package` weiterhin möglich.

18. **Versionsband-Konsistenz.** `Config.HelmImage`, `K8sVersion` und
    die Konstanten in `Dockerfile`/`flake.nix` driften jetzt
    unabhängig auseinander. Ein kleiner Helper-Test der die drei Werte
    abgleicht, hilft beim nächsten Bump.

**T-5 (P1.new)**: `scm.*` und `multiTenant.*` typisieren. Aktuell
liegen beide als `map[string]any` (Raw) im Go-Schema; der
Strict-Decode-Test in `internal/config/profile_compat_test.go` fängt
Tippfehler in diesen Bereichen NICHT, weil yaml.v3 alle Sub-Keys
schluckt. Das Groovy-Schema definiert konkrete Felder
(`scm.scmProviderType`, `scm.scmManager.{url,username,password,
namespace,skipPlugins,skipRestart,gitOpsUsername,helm}`,
`scm.gitlab.{url,username,password,parentGroupId,internal,
gitOpsUsername}`, `multiTenant.{useDedicatedInstance,scmProviderType,
centralArgocdNamespace,scmManager.*,gitlab.*}`). Vorgehen: typisierte
Structs analog zu `features.argocd`, `addScmConfig` /
`setMultiTenantModeConfig` auf die Structs umstellen, wire
aktualisieren. Erst danach ist die Profile-/Config-Kompatibilität
strikt geprüft.

**T-6 (P3.new)**: `Credentials`-Felder wurden in phase 12 von
`secretRef/secretKey/secretField` auf die Groovy-Form
`secretNamespace/secretName/usernameKey/passwordKey` umgestellt — der
ContentLoader-Code in `internal/content/*` referenziert diese Felder
aber noch nicht. Bei der Implementierung von P1.4 (Content COPY) mit
auf die neuen Namen achten.

**T-7 (P1.new)**: ArgoCD-Template-Tree (`argocd/cluster-resources/`)
in das Binary embedden statt vom Disk lesen. Aktuell verweist
`internal/features/argocd/setup.go` auf den relativen Pfad
`argocd/cluster-resources/`, der nur existiert wenn entweder der
Maven-Tree gerendert ist (`retired/argocd/cluster-resources/`) oder
ein Symlink gesetzt wurde. Fix: `//go:embed argocd/cluster-resources`
im Argo-Feature plus `copyFromFS` analog zum profile-Loader.

**T-8 (P1.new)**: SCMM-Setup-Configs (`namespaceStrategy:
CustomNamespaceStrategy`, baseUrl, plugin-installs) in das Go-Feature
einziehen. Aktuell wird das in der externalSCMM-Variante übersprungen
und der User muss `PUT /scm/api/v2/config` selbst absetzen — siehe
TEST_PLAN iteration 3. Die `Configure(ctx)`-Methode in
`internal/features/scmmanager/setup.go` ist nur dann reichbar, wenn
SCMM als internal markiert ist, was im Out-of-Cluster-Lauf nicht
funktioniert.

**T-9 (P1.new)**: ArgoCD-`Install`-Pfad pusht das Repo, lässt die
helm-Install plus den `argocd-secret`-bcrypt-Patch aber aus (siehe
Doc-Kommentar in `internal/features/argocd/argocd.go:Install`). Mit
dem Push allein bleibt das Cluster nach `gop --profile=minimal` ohne
laufendes Argo CD. Fix: helm `repo add` + `dependency build` +
`upgrade -i` plus Secret-Patch.

**T-10 (P2.new)**: Lokal-E2E-Modus. Wenn gop von außerhalb des
Clusters läuft, müssen Out-of-Cluster-URLs für SCMM/Jenkins
verwendet werden. Heute geht das nur via `scm.scmManager.url`
override (was die internal-install-Logik ausschaltet). Sauberer wäre
ein `application.externalAccess: true` Flag oder eine
Port-Forward-Hilfsfunktion.

## Empfohlene Reihenfolge

T-2 (Dockerfile SHA256s) → P1.5 (`monitoring` OpenShift-UID) →
P1.6 (`PersistConfig`) → P1.4 + P1.8 (ContentLoader COPY/FOLDER_BASED
+ HelmReleases) → P2.10 (E2E gegen Fake-Cluster).

P3 und P4 können parallel / auf Wunsch des Maintainers laufen.

## Audit-Befund (Boot-Sequenz, 2026-06-12)

Ein Doku-/Code-Audit nach den Phasen 0–11 hat eine Code-Auffälligkeit
zutage gefördert, die hier als Task ankommt:

~~T-4 (P2.new): `gitlab.Client.RepoURL` ruft `context.Background()`.~~
→ done in phase 12 (Option 2): `gitlab.Config.ParentFullPath` ist neu;
wenn der Wire-Caller den Pfad voraus-resolved (via
`Client.ResolveParentFullPath(ctx)`), trifft `RepoURL` keinen
HTTP-Endpunkt mehr. Test in `repourl_test.go`. Der Code-Pfad zum
Fallback bleibt bestehen, ist aber explizit als "wire-time preferred"
dokumentiert. Der originale Empfehlungsvorschlag:

**T-4 (P2.new)**: `internal/scm/gitlab.Client.RepoURL` ruft
`c.parentFullPath(context.Background())` ([gitlab.go:92](internal/scm/gitlab/gitlab.go))
weil das Provider-Interface kein `ctx` für `RepoURL` hat. Der zweite
Aufruf trifft den Cache, der erste macht jedoch einen HTTP-Round-Trip
ohne Context — verstößt im Geiste gegen §4.5. Drei Optionen:

1. `scm.Provider.RepoURL` um `ctx context.Context` als ersten
   Parameter ergänzen (Breaking Change, ein Konsument: `internal/wire`).
2. Den Parent-Path beim Wire-Build einmalig auflösen und über
   `gitlab.Config.ParentPath` injizieren (kein Interface-Change, dafür
   eager Lookup beim Start).
3. So lassen und im Doc-Kommentar warnen (Status quo, aber Stolperfalle
   für künftige Agents).

Empfohlen: **Option 2**, weil sie das Interface stabil hält und die
HTTP-Latenz nur einmal beim Bootstrap anfällt.

**T-5 (P1.new)**: `scm.*` und `multiTenant.*` typisieren. Aktuell
liegen beide als `map[string]any` (Raw) im Go-Schema; der
Strict-Decode-Test in `internal/config/profile_compat_test.go` fängt
Tippfehler in diesen Bereichen NICHT, weil yaml.v3 alle Sub-Keys
schluckt. Das Groovy-Schema definiert:

- `scm.scmProviderType` (Enum: SCM_MANAGER, GITLAB)
- `scm.scmManager.{url, username, password, namespace, skipPlugins,
  skipRestart, gitOpsUsername, helm}`
- `scm.gitlab.{url, username, password, parentGroupId, internal,
  gitOpsUsername}`
- `multiTenant.{useDedicatedInstance, scmProviderType,
  centralArgocdNamespace}` plus eigene `scmManager.*` und `gitlab.*`
  Sub-Bäume.

Vorgehen: typisierte Structs in `internal/config/schema.go` analog zu
`features.argocd` etc., plus `addScmConfig`/`setMultiTenantModeConfig`
in `internal/config/configurator.go` auf die Structs umstellen. Wire
in `internal/wire/wire.go` aktualisieren.
