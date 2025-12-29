package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/registry"
)

// newCacheCmd creates the cache command
func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage orla cache",
		Long: `Manage orla's cache. The cache stores registry indexes and cloned registry
repositories to speed up operations.`,
	}

	cmd.AddCommand(newCacheClearCmd())

	return cmd
}

// newCacheClearCmd creates the cache clear command
func newCacheClearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear the registry cache",
		Long: `Clear the registry cache. This will remove all cached registry indexes and
cloned registry repositories. The cache will be rebuilt automatically on the next
registry operation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := registry.ClearRegistryCache(); err != nil {
				return fmt.Errorf("failed to clear cache: %w", err)
			}
			return nil
		},
	}

	return cmd
}
