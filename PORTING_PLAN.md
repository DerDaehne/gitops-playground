# GitOps Playground – Portierungsplan Groovy → Go

Dieses Dokument beschreibt den geplanten Ansatz, um die Funktionalität des
Groovy-basierten gitops-playground-CLI nach Go (Verzeichnis `go_src/`) zu
portieren. Es dient als Leitfaden für die schrittweise Migration, die
Reduktion struktureller Komplexität und die Behebung von Fehlern, die im
bestehenden Groovy-Code gefunden werden.

## 1. Status Quo der Quell-Anwendung

Die bestehende Anwendung ist ein Micronaut-/Picocli-CLI in Groovy.

- ~9.653 Zeilen Groovy in 81 Quelldateien.
- Build mit Maven; Hauptklasse `com.cloudogu.gitops.cli.GitopsPlaygroundCliMain`.
- Zentrale Abhängigkeiten:
  - **Micronaut** – Dependency Injection, Singleton-Lifecycle.
  - **Picocli** – CLI-Parsing und Hilfe-Texte.
  - **Jackson / SnakeYAML** – YAML/JSON Schema-Handling.
  - **Fabric8 Kubernetes-Client** – K8s-API.
  - **JGit** – Git-Operationen.
  - **OkHttp + Retrofit** – HTTP-Aufrufe gegen SCM-Manager und Jenkins.
  - **Freemarker** – Templating für Helm-Values und Manifeste.
  - **Logback + SLF4J** – Logging.
  - **Spring Security Crypto** – Passwort-Hashing.
- Externe Tools, die per Subprozess aufgerufen werden: `helm`, `kubectl`,
  `git` (teilweise) und `curl` (in init-Skripten).

### Architektur in Schichten

```
cli ──> application ──> tools (Features) ──> infrastructure
                  │                       └─> deployment / helm / git / jenkins / k8s
                  └─> destroy (Destructor-Handler)
config (Schema, Profile, Validation)
utils  (FileSystem, Templating, MapUtils, NetworkingUtils, CommandExecutor, …)
dependencyinjection (HTTP-Client-Factories, Interceptoren)
```

Wichtige Kern-Klassen:

| Datei | Verantwortlichkeit |
| --- | --- |
| `cli/GitopsPlaygroundCli.groovy` | Entry-Point, Picocli-Parsing, Mergen von CLI/File/ConfigMap, Profile-Loading, Lebenszyklus. |
| `cli/ApplicationConfigurator.groovy` | Anreicherung des Konfigurations-Objekts mit abgeleiteten Werten. |
| `config/Config.groovy` | Globale Konfiguration (>700 LOC) mit verschachtelten Schemata. |
| `application/Application.groovy` | Iteration über alle `Tool`-Features, Aufruf `validate` und `install`. |
| `tools/common/Tool.groovy` | Abstrakte Feature-Basis, Helm-Deployment-Hook. |
| `tools/*` | Konkrete Features (ArgoCD, Jenkins, ScmManager, Monitoring, Vault, ESO, CertManager, Ingress, Registry). |
| `infrastructure/kubernetes/api/K8sClient.groovy` | Wrapper über Fabric8 (~1.224 LOC, viele Wartezyklen, Retry-Logik). |
| `infrastructure/git/*` | Repo-Anlage und Push gegen SCM-Manager / GitLab. |
| `infrastructure/jenkins/*` | Jenkins-API (Job-Erstellung, Plugins, globale Properties). |
| `infrastructure/helm/HelmClient.groovy` | Wrapper um `helm`-CLI. |
| `application/content/ContentLoader.groovy` | Laden externer Inhalte (Repos, Manifeste). |
| `destroy/Destroyer.groovy` | Orchestriert mehrere `DestructionHandler`. |

### Beobachtete Komplexitäts- und Fehlerquellen

Diese Punkte werden bei der Go-Migration adressiert (oder im Groovy-Code in
einem späteren Commit zumindest dokumentiert):

1. **K8sClient (1.224 LOC)** – mischt Wartezyklen, kubectl-Fallbacks und
   Fabric8-API in einer einzigen Klasse. Lässt sich in Go durch
   `client-go` + kleinere fokussierte Pakete (`pods`, `secrets`,
   `namespaces`, …) aufteilen.
2. **CommandExecutor** – pipet `Process`-Objekte über Groovy-Erweiterungen;
   die Pipe-Variante leidet an Race-Conditions (Kommentar im Code
   bestätigt: „concurrency 🤷“). In Go mit `os/exec.Cmd` und expliziten
   `io.Pipe`s sauber lösbar.
3. **HttpClientFactory** – verwendet ein `SSL`-Context („SSL“ statt
   „TLS“), schaltet Zertifikatsprüfung und Hostname-Verifizierung
   bedingungslos ab (auch wenn `insecure=false` ist, sobald der globale
   Verifier gesetzt wird). Das ist ein latenter Security-Bug, der in Go
   eindeutig nur dann gelockert wird, wenn `insecure=true` ist.
4. **Konfigurations-Merging** – `deepMerge` und `deepMergeDefaults` mit
   Groovy-Magie (Maps + Reflection). In Go besser als typsicheres,
   explizites Strukturen-Merging.
5. **`GitopsPlaygroundCli.runHook`** – greift per `metaClass.getMetaMethod`
   auf optionale Hook-Methoden zu. In Go ersetzt durch ein klares
   Interface (`PreConfigInit`, `PostConfigInit`).
6. **`Tool.getActiveNamespaceFromFeature`** – Reflection auf eine evtl.
   vorhandene Property `namespace`. In Go via Interface lösen.
7. **Tuple/Mixed-Maps in Templates** – Freemarker-Templates greifen auf
   Groovy-Maps zu. Go nutzt `text/template` mit explizit definierten
   Daten-Strukturen, dadurch werden Template-Fehler bereits beim
   Kompilieren der Templates oder zur Laufzeit deutlich besser
   diagnostiziert.

## 2. Ziele der Go-Portierung

1. **Funktionale Parität für den Standard-Anwendungsfall** (Install und
   Destroy für die Profile `minimal`, `content-examples`, `full`).
2. **Vereinfachung**: weniger Klassen-Hierarchien, klare Pakete je
   Verantwortung, Interfaces statt Reflection.
3. **Statische Typisierung der Config** statt verschachtelter Map-Merging-
   Operationen.
4. **Testbarkeit**: alle externen Dienste (K8s, SCM, Jenkins) hinter
   Interfaces.
5. **Single-Binary-Build**, kein JVM-Runtime und kein Docker-Init.

Außerhalb des Scopes (zunächst):
- AOT-/native-image-Build.
- Web-/Server-Komponenten.
- ContentLoader-spezifische Pfade, die externe Cloud-Repos brauchen –
  zunächst nur Skeleton.
- Operator-Profile (`operator-*`) werden erst nach dem Basis-Port
  angegangen.

## 3. Ziel-Architektur in Go

```
go_src/
  cmd/
    gop/                 main.go (CLI-Bootstrap)
  internal/
    cli/                 Cobra-Commands, Picocli-Äquivalent
    config/              Strukturierte Config + YAML + Profile + Merge
    profile/             Eingebettete YAML-Profile via embed.FS
    log/                 strukturiertes Logging mit slog
    runner/              Application: Validate + Install + Destroy
    feature/             Feature-Interface + Registry
      argocd/
      jenkins/
      scmmanager/
      monitoring/
      vault/
      eso/
      ingress/
      certmanager/
      registry/
    k8s/                 Kubernetes-Wrapper (client-go) + Wait-Helper
    git/                 go-git-Wrapper
    helm/                Wrapper um helm-CLI
    scm/                 SCM-Manager + GitLab HTTP-Clients
    jenkins/             Jenkins-API
    httpx/               HTTP-Client Factory (insecure-Handling, Retry)
    template/            text/template-Wrapper
    fsutil/              Helper für Datei-/Pfad-Operationen
    exec/                Subprozess-Helper (helm, kubectl, git)
  templates/             eingebettete Manifest-/Wert-Templates (via embed)
  go.mod
  PORTING_PLAN.md        (dieses Dokument)
  README.md
```

Wichtige Design-Entscheidungen:

- **Kein DI-Framework**: Konstruktoren in `main` verdrahten die Komponenten;
  in Tests werden Dependencies einfach gemockt.
- **Cobra statt Picocli** – Idiomatisches Go-CLI mit gleichem Verhalten der
  wichtigsten Flags (`--config-file`, `--config-map`, `--profile`,
  `--destroy`, `--yes`, `--debug`, `--trace`).
- **Embed der Profile** (`application-*.yaml`) und Templates via
  `embed.FS`, kein klassenpfad-basierter Resource-Loader.
- **Feature-Interface**:
  ```go
  type Feature interface {
      Name() string
      IsEnabled(cfg *config.Config) bool
      Validate(ctx context.Context, cfg *config.Config) error
      Install(ctx context.Context, cfg *config.Config) error
      Disable(ctx context.Context, cfg *config.Config) error
      Namespace(cfg *config.Config) string // "" wenn keiner
  }
  ```
- **Hooks** als optionale Interfaces:
  ```go
  type PreConfigInit  interface{ PreConfigInit(cfg *config.Config) error }
  type PostConfigInit interface{ PostConfigInit(cfg *config.Config) error }
  ```
- **HTTP-Insecure** wirkt nur, wenn `cfg.Application.Insecure == true`
  (Bugfix gegenüber Groovy-Code).
- **K8sClient**-Funktionalität wird in mehrere kleinere Pakete aufgeteilt
  (`pods.WaitReady`, `secrets.Apply`, `namespaces.EnsureCreated`, …).
- **CommandExecutor**: dünner Wrapper um `os/exec`; pipes ohne Threads über
  `io.Pipe`.

## 4. Phasen der Umsetzung

Jede Phase wird in einem eigenen Commit (oder einer Serie) im Branch
`feature/go_port` festgehalten.

### Phase 0 – Vorbereitung (dieser Commit)
- `feature/go_port` anlegen.
- `go_src/` mit `PORTING_PLAN.md` initialisieren.

### Phase 1 – Skelett & CLI
- `go.mod` mit Module-Namen `github.com/cloudogu/gitops-playground/go`.
- `cmd/gop/main.go` + `internal/cli` (Cobra) – Flags parsen, `--version`,
  `--help` und `--output-config-file` bedienen.
- Embed der Profile-YAMLs.
- Logging mit `slog`, Pegel `--debug`, `--trace`.
- Wandelt die Help-Texte/Constants aus `ConfigConstants.groovy` in Go um.

### Phase 2 – Config & Schema
- `internal/config` – typsichere Strukturen für `Application`, `Jenkins`,
  `Scm`, `Registry`, `Features`, `Content`, `MultiTenant`.
- YAML-Parser inkl. Schema-Validierung (via `kin-openapi` oder
  `xeipuuv/gojsonschema`).
- Merge-Order: defaults → ConfigMap → ConfigFile → Profile → CLI-Flags
  (entsprechend Groovy-Logik).
- Profile-Loader aus eingebetteten YAMLs.
- Helper `Config.ToYAML(secretsMasked bool)`.

### Phase 3 – Infrastruktur-Adapter
- `internal/exec` – Subprozess-Helfer.
- `internal/httpx` – `http.Client` mit korrektem TLS-Handling und
  Retry-Transport.
- `internal/k8s` – kleine Module mit `client-go`.
- `internal/helm` – Wrapper um `helm`-Binary.
- `internal/git` – `go-git` + Provider-Adapter.
- `internal/scm/scmmanager` und `internal/scm/gitlab` – HTTP-Clients.
- `internal/jenkins` – Jenkins-API.

### Phase 4 – Application & Features
- `internal/runner` – Orchestrierung über Feature-Liste mit Ordering.
- Feature-Pakete inkrementell aus den Groovy-Klassen portieren:
  1. ScmManager (Voraussetzung für Argo CD).
  2. Argo CD (Kernfeature).
  3. Jenkins.
  4. Monitoring.
  5. Vault + ESO.
  6. Ingress, CertManager, Registry.

### Phase 5 – Destroy-Pfad
- `internal/destroy` mit eigenen Handlern (ArgoCD, Jenkins, ScmManager).

### Phase 6 – Tests & Tooling
- Unit-Tests pro Paket (Tabellen-Tests).
- `make build` / `make test` analog Maven.
- CI-Snippet (separater Workflow) als Optional.

## 5. Geplante Bugfixes & Vereinfachungen

| # | Fundort (Groovy) | Beschreibung | Fix in Go |
| - | - | - | - |
| 1 | `HttpClientFactory.buildOkHttpClient` | `HostnameVerifier` wird auch bei `insecure=false` auf „alles akzeptieren“ gesetzt. | Hostname-Check nur überspringen, wenn `cfg.Application.Insecure` gesetzt ist. |
| 2 | `HttpClientFactory.insecureSslContext` | Verwendet `SSL` als Protokoll (deprecated). | Standard-`tls.Config{InsecureSkipVerify: true}` nur im Insecure-Modus. |
| 3 | `CommandExecutor.execute(String[],String[])` | Race-Condition zwischen Pipe-Prozessen (siehe Kommentar im Quelltext). | `os/exec.Cmd` + `io.Pipe` mit synchronisierten `Wait`s. |
| 4 | `GitopsPlaygroundCli.runHook` | Reflection-basierter Hook-Aufruf, schlägt schweigend fehl, wenn Signatur abweicht. | Typsicheres Interface (`PreConfigInit` / `PostConfigInit`). |
| 5 | `Tool.getActiveNamespaceFromFeature` | Reflection auf `namespace`-Property. | Methode `Namespace(cfg) string` auf Feature-Interface. |
| 6 | `Config.generatePassword` | Liest 12 Mal `nextInt(62)` aber das Alphabet hat 66 Zeichen – die Sonderzeichen `!@$%&` werden nie verwendet. | `nextInt(len(alphabet))` analog in Go. |
| 7 | `Application.start` | `setNamespaceListToConfig` mutiert die übergebene Config und speichert dann unmittelbar das YAML – Reihenfolge ist verwirrend. | Reine Funktion `BuildNamespaces(cfg)` → vorher aufrufen, dann persistieren. |
| 8 | `K8sClient` Helpers wie `waitForResourceWithRetry` | Schluck/Logging-Pattern doppelt implementiert. | Gemeinsamer `wait.PollImmediateUntilWithContext`-Wrapper. |
| 9 | `MapUtils.deepMerge` | Mutiert Eingabe-Map; in Tests leicht zu verwechseln. | Pure Funktion mit Copy-on-Write Semantik. |

Fundstellen mit eindeutigen Code-Bugs werden parallel in einem
Begleit-Commit dokumentiert (Issue-Referenz bzw. TODO-Kommentar im
Groovy-Source) – ohne im selben Branch Groovy-Verhalten zu ändern, falls
das nicht ausdrücklich gewünscht ist.

## 6. Erfolgskriterien

- `go build ./...` baut ein Binary `gop`.
- `gop --help`, `gop --version` funktionieren.
- `gop --profile=minimal --output-config-file` erzeugt eine korrekte
  YAML-Ausgabe (Vergleich mit der Groovy-Ausgabe).
- Unit-Tests für Config-Merge, Profile-Loading und Feature-Aktivierung
  laufen grün.
- Für mindestens einen Feature-Pfad (ArgoCD oder ScmManager) gibt es
  einen End-to-End-Test mit Mock-K8s.

## 7. Nicht im Scope dieser Iteration

- Tatsächliche Cluster-Operationen End-to-End.
- Migration der bestehenden Integrationstests.
- Operator-Profile (siehe oben).
- ContentLoader für Cloud-Repos.

## 8. Vorgehensmodell

1. Plan reviewen lassen.
2. Jede Phase als eigenen Commit auf `feature/go_port` umsetzen.
3. Nach jeder Phase Build & Tests sicherstellen (`go build`, `go test`).
4. Vor Phase 4 abklären, ob Features als reine Wrapper über die externen
   `helm`/`kubectl`-CLIs portiert werden sollen (schneller, kompatibler
   zur bestehenden Logik) oder direkt über `client-go` (typsicher, aber
   höherer Aufwand). Empfehlung: hybrid – `client-go` für Reads/Waits,
   `helm` weiterhin als Subprozess für Charts.
