# Go-Module-Abhängigkeiten für `internal/scm`

Dieses Verzeichnis darf laut Auftrag `go.mod`/`go.sum` nicht selbst anpassen.
Die Adapter sind absichtlich nur mit der Go-Standardbibliothek umgesetzt
(`net/http`, `encoding/json`, `net/url`, `net/http/httptest` in Tests).

## Produktiv

Keine zusätzlichen Module nötig.

## Test

Keine zusätzlichen Module nötig.

## Begründung

Der Groovy-Code nutzt Retrofit + OkHttp + Jackson für SCM-Manager und
gitlab4j-api für GitLab. Beide Bibliotheken wurden bewusst nicht durch
ein direktes Go-Pendant ersetzt, weil:

1. die genutzten Endpunkte sehr klein sind (drei Endpunkte für
   SCM-Manager, sechs für GitLab),
2. die HTTP-Logik (Insecure-TLS, Retry, BasicAuth) bereits in
   `internal/httpx` zentralisiert ist – das Hinzufügen einer SDK-Schicht
   würde diese Garantien durchbrechen,
3. die Code-Generation aus OpenAPI-Specs für GitLab/SCM-Manager mehr
   Maintenance verursacht als die wenigen handgeschriebenen DTOs hier.
