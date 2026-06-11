package content

// STUB: deployHelmReleasesFromContent.
//
// The Groovy original walks cfg.Content.HelmReleases and calls
// Tool.deployHelmChart for each entry, merging an inline `values` map with
// an optional values file. In the Go port the same deploy invocation lives
// behind deployment.Strategy (see internal/deployment). The runner does not
// yet wire a Strategy onto the content Feature, so this file deliberately
// stays empty for now.
//
// Planned shape, once Strategy is wired:
//
//   func (f Feature) processHelmReleases(ctx context.Context, cfg *config.Config) error {
//       for _, hr := range cfg.Content.HelmReleases {
//           if err := ctx.Err(); err != nil { return err }
//           valuesPath, cleanup, err := deployment.RenderHelmValues(cfg, deployment.HelmValuesRequest{
//               InlineValues: hr.Values,
//               ExtraPaths:   []string{hr.ValuesPath},
//           })
//           if err != nil { return err }
//           defer cleanup()
//           if err := f.Deploy.Deploy(ctx, deployment.Spec{
//               RepoURL:        hr.RepoURL,
//               ChartOrPath:    hr.Chart,
//               Version:        firstNonEmpty(hr.Version, "*"),
//               Namespace:      hr.Namespace,
//               ReleaseName:    firstNonEmpty(hr.ReleaseName, hr.Name),
//               HelmValuesPath: valuesPath,
//               RepoType:       deployment.RepoHelm,
//           }); err != nil { return err }
//       }
//       return nil
//   }
//
// The placeholder in Feature.Install logs a Warn so users notice the
// missing functionality without crashing.
