# Go-Module-Abhängigkeiten für `internal/k8s`

Dieses Verzeichnis darf laut Auftrag `go.mod`/`go.sum` nicht selbst anpassen.
Die folgende Liste gibt die exakten `require`-Zeilen wieder, die der
Integrator in `go_src/go.mod` ergänzen muss, damit der Adapter baut.

## Produktiv

```
require (
    k8s.io/api v0.31.1
    k8s.io/apimachinery v0.31.1
    k8s.io/client-go v0.31.1
    sigs.k8s.io/yaml v1.4.0
)
```

Begründung pro Modul:

- `k8s.io/client-go` – typed clients (CoreV1, AppsV1), dynamic-client für
  generische Apply-/Patch-Operationen, `rest.InClusterConfig`, `tools/clientcmd`
  für Kubeconfig-Lookup, Fake-Client für Tests.
- `k8s.io/apimachinery` – `metav1`, `runtime.Object`, `schema.GroupVersionResource`,
  `wait.PollImmediateUntilWithContext` (über `apimachinery/pkg/util/wait`).
- `k8s.io/api` – typisierte Resource-Typen (`corev1.Pod`, `corev1.Secret`, …).
- `sigs.k8s.io/yaml` – YAML→JSON-Konvertierung für Multi-Doc-Apply.

Die transitiv mitgezogenen Module (`golang.org/x/*`, `github.com/go-logr/*`,
`github.com/google/gnostic-models`, `github.com/json-iterator/go`,
`github.com/modern-go/*`, `gopkg.in/inf.v0`, `k8s.io/klog/v2`,
`k8s.io/utils`, `sigs.k8s.io/json`, `sigs.k8s.io/structured-merge-diff/v4`)
werden von `go mod tidy` automatisch in `go.sum` aufgenommen und müssen
nicht manuell eingetragen werden.

Hinweis: Wir bleiben bewusst auf v0.31.x (Kubernetes 1.31), weil das die
letzte Minor-Version ist, die noch `go 1.22` als Mindeststand erlaubt –
passend zum aktuellen `go.mod`.

## Test (bereits Teil des Produktiv-Bundles)

- `k8s.io/client-go/kubernetes/fake` – Fake-Clientset für Unit-Tests, kommt
  mit `client-go` und braucht keinen zusätzlichen Eintrag in `go.mod`.
- `k8s.io/client-go/dynamic/fake` – Fake-Dynamic-Client für Apply-Tests,
  ebenfalls Teil von `client-go`.
