# Adapter-Spezifikationen (Phase 3)

Dieses Dokument beschreibt, was jeder Go-Adapter aus dem Groovy-Original
übernimmt, was bewusst weggelassen oder vereinfacht wird, und welche Bugs
beim Port behoben werden. Jeder Abschnitt ist als Brief für die
Implementierung gedacht – Foundation-Adapter werden inline umgesetzt,
die größeren bekommen Sub-Agents.

---

## `internal/exec` – Subprozess-Wrapper

**Quelle**: `com.cloudogu.gitops.utils.CommandExecutor`.

**Vereinfachte API**:

```go
type Runner interface {
    Run(ctx context.Context, name string, args ...string) (Output, error)
    RunEnv(ctx context.Context, env map[string]string, name string, args ...string) (Output, error)
    Pipe(ctx context.Context, first, second Command) (Output, error)
}
type Output struct { Stdout, Stderr string; ExitCode int }
type Command struct { Name string; Args []string; Env map[string]string }
```

**Bugfixes/Vereinfachungen gegenüber Groovy**:
- Pipe-Variante mit `io.Pipe` und expliziten `Wait`-Aufrufen → keine
  Race-Condition mehr (`// concurrency 🤷` Kommentar entfällt).
- Timeout per Context statt globaler `PROCESS_TIMEOUT_MINUTES`-Konstante.
- Trace-Logging via `slog` mit Trace-Level.

---

## `internal/httpx` – HTTP-Client-Factory

**Quelle**: `HttpClientFactory.groovy`, `RetryInterceptor.groovy`,
`AuthorizationInterceptor.groovy`.

**API**:

```go
func New(opts Options) *http.Client
type Options struct {
    Insecure      bool          // when true: skip TLS verify
    BasicAuth     *BasicAuth    // user:pw header injection
    Retry         RetryPolicy   // max attempts + backoff
    Timeout       time.Duration
    CookieJar     bool
}
type BasicAuth struct{ User, Pass string }
type RetryPolicy struct{ MaxAttempts int; BaseDelay time.Duration }
```

**Bugfixes**:
1. `Insecure` schaltet ausschließlich TLS-Verify aus, wenn explizit
   gesetzt. Im Groovy-Code wird zusätzlich der HostnameVerifier global
   auf `true` gesetzt – das passiert in der Go-Version nicht.
2. Statt `SSLContext.getInstance("SSL")` (deprecated) verwenden wir den
   Standard-`tls.Config{InsecureSkipVerify: true}`.
3. Retry: einfacher RoundTripper-Wrap, idempotente Methoden (GET, HEAD,
   PUT bei 5xx) bis MaxAttempts; alle anderen Statuscodes durchgereicht.
4. Auth-Logging: Authorization-Header beim Trace-Logging redacted.

---

## `internal/template` – Templating

**Quelle**: `TemplatingEngine.groovy` (Freemarker).

**Begründung der Verlagerung**: Freemarker hat eigene Syntax; wir
wechseln auf Go-`text/template`. Damit verändern sich die Templates der
Manifeste. Wir bewahren die alten `.ftl`-Dateien unverändert, indem wir
sie *zusätzlich* zu `.tmpl`-Pendants halten oder sie 1:1 syntaktisch
übernehmen, wenn das ohne Verlust geht (siehe Bemerkungen in den
Feature-Specs Phase 4).

**API**:

```go
func RenderString(name, body string, data any) (string, error)
func RenderFile(path string, data any) (string, error)
func RenderTree(srcDir, dstDir string, data any, opts RenderOptions) error
type RenderOptions struct {
    Suffix string   // default ".tmpl"; Templates remove that suffix on write
    Skip   func(path string) bool
}
```

**Vereinfachung**:
- Templates ohne Inhalt werden nicht geschrieben (entspricht
  `replaceTemplate` im Original).

---

## `internal/fsutil` – Datei-Helper

**Quelle**: `FileSystemUtils.groovy`.

**API** (nur was wirklich benötigt wird – die Linien-Such-Variationen aus
dem Groovy-Code werden gestrichen, weil sie nirgendwo außerhalb der
Tests genutzt werden):

```go
func CopyDir(src, dst string) error
func ReadYAML(path string, into any) error
func WriteYAML(path string, data any) error
func WriteTempYAML(data any) (path string, cleanup func(), err error)
func ReplaceInFile(path, from, to string) error
```

**Begründung der Streichungen**: `getLineFromFile`, `getSubstringOfFile`,
`getAllLinesFromFile`, `replaceFileContent`-Varianten – im
Produktionscode nicht gebraucht, in Tests durch `os.ReadFile +
strings.Contains` ersetzbar.

---

## `internal/k8s` – Kubernetes-Adapter

**Quelle**: `infrastructure/kubernetes/api/K8sClient.groovy` (1.224 LOC).

**Aufteilung**: Statt einer Monolith-Klasse mehrere fokussierte Pakete
unter `internal/k8s/`:

- `client.go` – `Client`-Wrapper um `client-go`, `current context`,
  `current namespace`.
- `nodes.go` – `WaitForNode`, `WaitForInternalIP`.
- `namespace.go` – `EnsureNamespace`, `DeleteNamespace`.
- `secrets.go` – `ApplyGenericSecret`, `ApplyDockerConfigSecret`,
  `DeleteSecret`.
- `configmaps.go` – `GetConfigMap`, `ApplyConfigMap`.
- `pods.go` – `WaitForPod`, `WaitUntilDeploymentReady`.
- `apply.go` – `Apply([]byte yaml)`, `Patch(...)` über
  `dynamic.Interface`.
- `wait.go` – gemeinsame `PollUntil`-Hilfsfunktion (entfernt das
  Duplikat-Pattern `waitForResourceWithRetry`).

**Bugfixes**:
- Timeouts kommen aus dem Context, nicht aus Member-Variablen.
- Fehlerpfade liefern `fmt.Errorf("...: %w", err)` statt nur
  Logging.
- Kein `kubectl`-Subprozess-Fallback mehr (Fabric8-Original nutzt das in
  ein paar Pfaden) – `client-go` reicht.

**Externe Abhängigkeiten** (`go.mod`):
- `k8s.io/client-go`
- `k8s.io/apimachinery`
- `sigs.k8s.io/yaml` für Apply

**Test-Strategie**: kleines Interface `internal/k8s.Reader/Writer`, in
Feature-Tests durch eine In-Memory-Implementierung ersetzbar.

---

## `internal/helm` – Helm-CLI-Wrapper

**Quelle**: `infrastructure/helm/HelmClient.groovy`.

**API**:

```go
type Client struct{ Runner exec.Runner }
func (Client) AddRepo(ctx, name, url string) error
func (Client) DependencyBuild(ctx, path string) error
func (Client) Upgrade(ctx, release, chartOrPath string, opts UpgradeOptions) error
func (Client) Template(ctx, release, chartOrPath string, opts UpgradeOptions) (string, error)
func (Client) Uninstall(ctx, release, namespace string) error

type UpgradeOptions struct {
    Namespace string
    Version   string
    Values    []string // file paths
    Set       map[string]string
    CreateNS  bool
    ExtraArgs []string
}
```

**Bugfixes**:
- Args werden als Slice übergeben, nicht aus einer Map (= keine Sortier-
  Abhängigkeit, was im Groovy-Original schmerzhaft war).
- Fehler enthalten stderr.

---

## `internal/git` – Git-Operationen

**Quelle**: `infrastructure/git/GitRepo.groovy` + JGit Helpers.

**Abhängigkeit**: `github.com/go-git/go-git/v5`.

**API**:

```go
type Repo struct {
    Dir       string
    Auth      Auth
    Author    Identity
    Insecure  bool
}
type Auth struct{ Username, Password string }
type Identity struct{ Name, Email string }

func Clone(ctx, url string, opts CloneOptions) (*Repo, error)
func (r *Repo) Commit(msg string, paths ...string) error
func (r *Repo) Push(ctx context.Context, opts PushOptions) error
func (r *Repo) Checkout(ctx context.Context, ref string) error
func (r *Repo) ListBranches(ctx context.Context) ([]string, error)
func (r *Repo) ReadFile(ref, path string) ([]byte, error)
```

**Bugfixes/Vereinfachungen**:
- `InsecureCredentialProvider` (JGit) → in go-git ist
  `transport.UnsupportedCapabilities` + `InsecureSkipTLS=true`.
- `GitRepo` aus dem Groovy-Code mischt Git und SCM-Provider; hier
  trennen wir das: Git nur Git, der Provider-Teil zieht nach
  `internal/scm`.

---

## `internal/scm` – SCM Manager + GitLab API

**Quellen**:
- `infrastructure/git/providers/scmmanager/api/*`
- `infrastructure/git/providers/scmmanager/ScmManager.groovy`
- `infrastructure/git/providers/scmmanager/Permission.groovy`
- `infrastructure/git/providers/scmmanager/ScmManagerUrlResolver.groovy`
- `infrastructure/git/providers/gitlab/Gitlab.groovy`
- `infrastructure/git/providers/GitProvider.groovy`

**API**:

```go
type Provider interface {
    Name() string
    CreateRepository(ctx, ns, name, description string, init bool) (created bool, err error)
    SetRepositoryPermission(ctx, ns, name, principal string, role Role, scope Scope) error
    EnsureUser(ctx, login, password, display, mail string) error
    EnsureNamespace(ctx, name string) error
    RepoURL(ns, name string) string
    GitOpsUsername() string
}
type Role  int8 // READ, WRITE, OWNER
type Scope int8 // USER, GROUP
```

Provider-Implementierungen:
- `scmmanager.New(cfg, httpClient)` – REST gegen SCM-Manager v2/v3.
- `gitlab.New(cfg, httpClient)` – REST gegen GitLab.

**Bugfix-Hinweise**:
- SCM-Manager-Plugin-Aktivierung im Groovy macht ein blockierendes
  Polling; wir steuern das via Context-Cancellation.
- URL-Resolver: konsolidiert in `urlresolver.go`, statt mehrere
  überladene Methoden mit `String`-Konkatenation.

---

## `internal/jenkins` – Jenkins-API

**Quellen**:
- `infrastructure/jenkins/JenkinsApiClient.groovy`
- `JobManager.groovy`, `UserManager.groovy`, `GlobalPropertyManager.groovy`,
  `PrometheusConfigurator.groovy`.

**API**:

```go
type Client struct {
    BaseURL string
    User    string
    Pass    string
    HTTP    *http.Client
}
func (c *Client) CreateOrUpdateJob(ctx, folder, name, configXML string) error
func (c *Client) RunJob(ctx, fullName string, params map[string]string) error
func (c *Client) EnsureUser(ctx, login, password, displayName, mail string) error
func (c *Client) SetGlobalProperty(ctx, key, value string) error
func (c *Client) ConfigurePrometheus(ctx context.Context, p PrometheusConfig) error
func (c *Client) RestartSafely(ctx context.Context) error
```

**Bugfixes**:
- CSRF-Crumb wird vor jedem mutierenden Call automatisch geholt
  (Groovy-Implementierung vergisst das in Edge-Cases).
- Jenkins-Restart wartet auf einen klar definierten Health-Endpoint,
  nicht auf einen ad-hoc Sleep.

---

## Reihenfolge der Umsetzung

1. **Foundation (inline durch Hauptagent)**: `exec`, `httpx`, `template`,
   `fsutil`.
2. **Sub-Agents parallel**: `k8s`, `helm`, `git`, `scm`, `jenkins`.

Jeder Sub-Agent erhält den vollen Pfad zu dieser Spezifikation und der
zugehörigen Groovy-Quellen als Briefing.
