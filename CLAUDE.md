# gitops-playground (Go Rewrite)

KI-beschleunigter Go-Rewrite des Groovy/Micronaut gitops-playground.

## Architektur-Prinzipien

- **Feature-Interface** mit `DependsOn()` — kein `@Order` mehr
- **DAG-Executor** in `internal/pipeline/` — parallele Ausführung unabhängiger Features
- **Zweiphasiger Bootstrap**: GitHandler + ArgoCD imperativ (Phase 1), alle anderen Features als ArgoCD Applications (Phase 2)
- **Per-Feature-Pakete**: jedes Feature hat sein eigenes Unterpaket in `internal/feature/<name>/`

## Dependency-Graph der Features

```
Phase 1 (imperativ):
  githandler → argocd

Phase 2 (via ArgoCD):
  argocd → registry
  argocd + githandler → jenkins
  argocd → ingress
  argocd + ingress → certmanager
  argocd + githandler → monitoring
  argocd → vault
  argocd + vault → eso
  argocd + jenkins → content
  argocd → mail
```

## Groovy-Referenz

Die vollständige Architektur-Dokumentation liegt in `../Groovy/gitops-playground/docs/architecture/`.
Neue Features immer erst dort dokumentieren, dann hier implementieren.

## Neues Feature hinzufügen

1. `internal/feature/<name>/<name>.go` erstellen — `DependsOn()` korrekt deklarieren
2. `internal/config/config.go` erweitern (oder neue Datei in `internal/config/`)
3. In `cmd/apply.go` in `allFeatures()` registrieren
4. KI-Prompt-Vorlage: Referenz-Feature `internal/feature/registry/registry.go` zeigen

## Build

```sh
nix develop     # Entwicklungsumgebung (Go, kubectl, helm, k9s)
go build ./...
go test ./...
nix build       # Vollständiges Binary via Nix
```

## Code-Konventionen

- **Error Handling**: `fmt.Errorf("...: %w", err)` (mit Wrapping für Stack-Traces)
- **Logging**: `slog.Info()`, `slog.Debug()`, `slog.Warn()`, `slog.Error()`
- **Receiver-Namen**: Kurz (`sm`, `gl`, `g`, `r`, `f`)
- **Imports**: stdlib, dann third-party, dann internal
- **Sprache**: Code und Kommentare auf Englisch, Planung/Kommunikation auf Deutsch
- **HTTP APIs**: `handle201or409` Pattern (201=created, 409=exists=ok, sonst error)
