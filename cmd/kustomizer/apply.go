package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/kustomize/api/filesys"

	"github.com/stefanprodan/kustomizer/pkg/engine"
)

var applyCmd = &cobra.Command{
	Use:   "apply [path]",
	Short: "Run kustomization and prune previous applied Kubernetes objects",
	RunE:  applyCmdRun,
}

var (
	group        string
	name         string
	revision     string
	timeout      time.Duration
	cfgNamespace string
)

func init() {
	applyCmd.Flags().StringVar(&group, "group", "kustomizer", "group")
	applyCmd.Flags().StringVarP(&name, "name", "", "", "name")
	applyCmd.Flags().StringVarP(&revision, "revision", "r", "", "revision")
	applyCmd.Flags().StringVarP(&cfgNamespace, "gc-namespace", "", "default", "namespace to store the GC snapshot ConfigMap")
	applyCmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "timeout for this operation")

	rootCmd.AddCommand(applyCmd)
}

func applyCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("path is required")
	}
	base := args[0]
	fs := filesys.MakeFsOnDisk()

	tmpDir, err := ioutil.TempDir("", name)
	if err != nil {
		return fmt.Errorf("tmp dir error: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if !strings.HasSuffix(base, "/") {
		base += "/"
	}

	c := fmt.Sprintf("cp -r %s* %s", base, tmpDir)
	command := exec.Command("/bin/sh", "-c", c)
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s command failed", c)
	}

	base = tmpDir

	revisor, err := engine.NewRevisior(group, name, revision)
	if err != nil {
		return err
	}

	transformer, err := engine.NewTransformer(fs, revisor)
	if err != nil {
		return err
	}
	err = transformer.Generate(base)
	if err != nil {
		return err
	}

	generator, err := engine.NewGenerator(fs, revisor)
	if err != nil {
		return err
	}
	err = generator.Generate(base)
	if err != nil {
		return err
	}

	builder, err := engine.NewBuilder(fs)
	if err != nil {
		return err
	}

	manifests := filepath.Join(base, revisor.ManifestFile())
	err = builder.Generate(base, manifests)
	if err != nil {
		return err
	}

	applier, err := engine.NewApplier(fs, revisor, timeout)
	if err != nil {
		return err
	}

	err = applier.Run(manifests, false)
	if err != nil {
		return err
	}

	gc, err := engine.NewGarbageCollector(revisor, timeout)
	if err != nil {
		return err
	}

	write := func(obj string) {
		if !strings.Contains(obj, "No resources found") {
			fmt.Println(obj)
		}
	}

	err = gc.Run(manifests, cfgNamespace, write)
	if err != nil {
		return err
	}

	return nil
}