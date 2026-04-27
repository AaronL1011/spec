package cmd

import (
	"context"
	"fmt"

	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/pipeline"
	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy [id]",
	Short: "Trigger deployment via CI/CD adapter",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDeploy,
}

func init() {
	deployCmd.Flags().String("env", "staging", "target environment")
	rootCmd.AddCommand(deployCmd)
}

func runDeploy(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec deploy <id>")
	if err != nil {
		return err
	}
	env, _ := cmd.Flags().GetString("env")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	if !rc.HasIntegration("deploy") {
		return fmt.Errorf("deploy integration not configured — add 'integrations.deploy' to spec.config.yaml")
	}

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	meta, err := markdown.ReadMeta(path)
	if err != nil {
		return err
	}

	if len(meta.Repos) == 0 {
		return fmt.Errorf("no repos listed in %s — add 'repos:' to the frontmatter", specID)
	}

	reg := buildRegistry(rc)

	fmt.Printf("Deploying %s to %s...\n", specID, env)
	for _, repo := range meta.Repos {
		fmt.Printf("  Triggering %s...\n", repo)
	}

	run, err := reg.Deploy().Trigger(ctx(), meta.Repos, env)
	if err != nil {
		return fmt.Errorf("triggering deployment: %w", err)
	}

	if run != nil && run.URL != "" {
		fmt.Printf("✓ Deployment triggered: %s\n", run.URL)
	} else {
		fmt.Println("✓ Deployment triggered.")
	}

	// Transition spec to 'deploying' if post-merge stages are configured
	pl := rc.Pipeline()
	if pl.StageByName("deploying") != nil && meta.Status == "done" {
		err = gitpkg.WithSpecsRepo(context.Background(), &rc.Team.SpecsRepo, func(repoPath string) (string, error) {
			specPath, err := specPathIn(repoPath, rc, specID)
			if err != nil {
				return "", err
			}
			latestMeta, err := markdown.ReadMeta(specPath)
			if err != nil {
				return "", err
			}
			if _, err := pipeline.Advance(specPath, latestMeta, "deploying"); err != nil {
				return "", err
			}
			return fmt.Sprintf("feat: deploy %s to %s", specID, env), nil
		})
		if err != nil {
			warnf("could not transition to deploying: %v", err)
		} else {
			fmt.Printf("✓ %s transitioned to deploying\n", specID)
		}
	}

	return nil
}
