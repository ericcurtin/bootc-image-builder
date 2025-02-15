package main_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	main "github.com/osbuild/bootc-image-builder/bib/cmd/bootc-image-builder"
	"github.com/osbuild/images/pkg/blueprint"
	"github.com/osbuild/images/pkg/container"
	"github.com/osbuild/images/pkg/manifest"
	"github.com/osbuild/images/pkg/rpmmd"
)

func TestCanChownInPathHappy(t *testing.T) {
	tmpdir := t.TempDir()
	canChown, err := main.CanChownInPath(tmpdir)
	require.Nil(t, err)
	assert.Equal(t, canChown, true)

	// no tmpfile leftover
	content, err := os.ReadDir(tmpdir)
	require.Nil(t, err)
	assert.Equal(t, len(content), 0)
}

func TestCanChownInPathNotExists(t *testing.T) {
	canChown, err := main.CanChownInPath("/does/not/exists")
	assert.Equal(t, canChown, false)
	assert.ErrorContains(t, err, ": no such file or directory")
}

func TestCanChownInPathCannotChange(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot run as root (fchown never errors here)")
	}

	restore := main.MockOsGetuid(func() int {
		return -2
	})
	defer restore()

	tmpdir := t.TempDir()
	canChown, err := main.CanChownInPath(tmpdir)
	require.Nil(t, err)
	assert.Equal(t, canChown, false)
}

type manifestTestCase struct {
	config     *main.ManifestConfig
	imageType  string
	packages   map[string][]rpmmd.PackageSpec
	containers map[string][]container.Spec
	expStages  map[string][]string
	nexpStages map[string][]string
	err        interface{}
}

func getBaseConfig() *main.ManifestConfig {
	return &main.ManifestConfig{Imgref: "testempty"}
}

func getUserConfig() *main.ManifestConfig {
	// add a user
	pass := "super-secret-password-42"
	key := "ssh-ed25519 AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return &main.ManifestConfig{
		Imgref:  "testuser",
		ImgType: "",
		Config: &main.BuildConfig{
			Blueprint: &blueprint.Blueprint{
				Customizations: &blueprint.Customizations{
					User: []blueprint.UserCustomization{
						{
							Name:     "tester",
							Password: &pass,
							Key:      &key,
						},
					},
				},
			},
		},
	}
}

func TestManifestGenerationEmptyConfig(t *testing.T) {
	baseConfig := getBaseConfig()
	testCases := map[string]manifestTestCase{
		"ami-base": {
			config:    baseConfig,
			imageType: "ami",
		},
		"raw-base": {
			config:    baseConfig,
			imageType: "raw",
		},
		"qcow2-base": {
			config:    baseConfig,
			imageType: "qcow2",
		},
		"iso-base": {
			config:    baseConfig,
			imageType: "iso",
		},
		"empty-config": {
			config:    &main.ManifestConfig{},
			imageType: "qcow2",
			err:       errors.New("pipeline: no base image defined"),
		},
		"bad-image-type": {
			config:    baseConfig,
			imageType: "bad",
			err:       errors.New("Manifest(): unsupported image type \"bad\""),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			config := main.ManifestConfig(*tc.config)
			config.ImgType = tc.imageType
			_, err := main.Manifest(&config)
			assert.Equal(t, err, tc.err)
		})
	}
}

func TestManifestGenerationUserConfig(t *testing.T) {
	userConfig := getUserConfig()
	testCases := map[string]manifestTestCase{
		"ami-user": {
			config:    userConfig,
			imageType: "ami",
		},
		"raw-user": {
			config:    userConfig,
			imageType: "raw",
		},
		"qcow2-user": {
			config:    userConfig,
			imageType: "qcow2",
		},
		"iso-user": {
			config:    userConfig,
			imageType: "iso",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			config := main.ManifestConfig(*tc.config)
			config.ImgType = tc.imageType
			_, err := main.Manifest(&config)
			assert.NoError(t, err)
		})
	}
}

func TestManifestSerialization(t *testing.T) {
	// Tests that the manifest is generated without error and is serialized
	// with expected key stages.

	// Disk images require a container for the build pipeline and the ostree-deployment.
	containerSpec := container.Spec{
		Source:  "test-container",
		Digest:  "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		ImageID: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
	}
	diskContainers := map[string][]container.Spec{
		"build": {
			containerSpec,
		},
		"ostree-deployment": {
			containerSpec,
		},
	}

	// ISOs require a container for the bootiso-tree, build packages, and packages for the anaconda-tree (with a kernel).
	isoContainers := map[string][]container.Spec{
		"bootiso-tree": {
			containerSpec,
		},
	}
	isoPackages := map[string][]rpmmd.PackageSpec{
		"build": {
			{
				Name:     "package",
				Version:  "113",
				Checksum: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
		"anaconda-tree": {
			{
				Name:     "kernel",
				Version:  "10.11",
				Checksum: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			},
			{
				Name:     "package",
				Version:  "113",
				Checksum: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
	}

	pkgsNoBuild := map[string][]rpmmd.PackageSpec{
		"anaconda-tree": {
			{
				Name:     "kernel",
				Version:  "10.11",
				Checksum: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			},
			{
				Name:     "package",
				Version:  "113",
				Checksum: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
	}

	baseConfig := getBaseConfig()
	userConfig := getUserConfig()
	testCases := map[string]manifestTestCase{
		"ami-base": {
			config:     baseConfig,
			imageType:  "ami",
			containers: diskContainers,
			expStages: map[string][]string{
				"build": {"org.osbuild.container-deploy"},
				"ostree-deployment": {
					"org.osbuild.ostree.deploy.container",
				},
			},
			nexpStages: map[string][]string{
				"build": {"org.osbuild.rpm"},
				"ostree-deployment": {
					"org.osbuild.users",
				},
			},
		},
		"raw-base": {
			config:     baseConfig,
			imageType:  "raw",
			containers: diskContainers,
			expStages: map[string][]string{
				"build": {"org.osbuild.container-deploy"},
				"ostree-deployment": {
					"org.osbuild.ostree.deploy.container",
				},
			},
			nexpStages: map[string][]string{
				"build": {"org.osbuild.rpm"},
				"ostree-deployment": {
					"org.osbuild.users",
				},
			},
		},
		"qcow2-base": {
			config:     baseConfig,
			imageType:  "qcow2",
			containers: diskContainers,
			expStages: map[string][]string{
				"build": {"org.osbuild.container-deploy"},
				"ostree-deployment": {
					"org.osbuild.ostree.deploy.container",
				},
			},
			nexpStages: map[string][]string{
				"build": {"org.osbuild.rpm"},
				"ostree-deployment": {
					"org.osbuild.users",
				},
			},
		},
		"ami-user": {
			config:     userConfig,
			imageType:  "ami",
			containers: diskContainers,
			expStages: map[string][]string{
				"build": {"org.osbuild.container-deploy"},
				"ostree-deployment": {
					"org.osbuild.users",
					"org.osbuild.ostree.deploy.container",
				},
			},
			nexpStages: map[string][]string{
				"build": {"org.osbuild.rpm"},
			},
		},
		"raw-user": {
			config:     userConfig,
			imageType:  "raw",
			containers: diskContainers,
			expStages: map[string][]string{
				"build": {"org.osbuild.container-deploy"},
				"ostree-deployment": {
					"org.osbuild.users", // user creation stage when we add users
					"org.osbuild.ostree.deploy.container",
				},
			},
			nexpStages: map[string][]string{
				"build": {"org.osbuild.rpm"},
			},
		},
		"qcow2-user": {
			config:     userConfig,
			imageType:  "qcow2",
			containers: diskContainers,
			expStages: map[string][]string{
				"build": {"org.osbuild.container-deploy"},
				"ostree-deployment": {
					"org.osbuild.users", // user creation stage when we add users
					"org.osbuild.ostree.deploy.container",
				},
			},
			nexpStages: map[string][]string{
				"build": {"org.osbuild.rpm"},
			},
		},
		"iso-user": {
			config:     userConfig,
			imageType:  "iso",
			containers: isoContainers,
			packages:   isoPackages,
			expStages: map[string][]string{
				"build":        {"org.osbuild.rpm"},
				"bootiso-tree": {"org.osbuild.skopeo"}, // adds the container to the ISO tree
			},
		},
		"iso-nobuildpkg": {
			config:     userConfig,
			imageType:  "iso",
			containers: isoContainers,
			packages:   pkgsNoBuild,
			err:        "serialization not started",
		},
		"iso-nocontainer": {
			config:    userConfig,
			imageType: "iso",
			packages:  isoPackages,
			err:       "missing ostree, container, or ospipeline parameters in ISO tree pipeline",
		},
		"ami-nocontainer": {
			config:    userConfig,
			imageType: "ami",
			err:       "pipeline ostree-deployment requires exactly one ostree commit or one container (have commits: []; containers: [])",
		},
		"raw-nocontainer": {
			config:    userConfig,
			imageType: "raw",
			err:       "pipeline ostree-deployment requires exactly one ostree commit or one container (have commits: []; containers: [])",
		},
		"qcow2-nocontainer": {
			config:    userConfig,
			imageType: "qcow2",
			err:       "pipeline ostree-deployment requires exactly one ostree commit or one container (have commits: []; containers: [])",
		},
	}

	// Use an empty config: only the imgref is required
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			config := main.ManifestConfig(*tc.config)
			config.ImgType = tc.imageType
			mf, err := main.Manifest(&config)
			assert.NoError(err) // this isn't the error we're testing for

			if tc.err != nil {
				assert.PanicsWithValue(tc.err, func() {
					_, err := mf.Serialize(tc.packages, tc.containers, nil)
					assert.NoError(err)
				})
			} else {
				manifestJson, err := mf.Serialize(tc.packages, tc.containers, nil)
				assert.NoError(err)
				assert.NoError(checkStages(manifestJson, tc.expStages, tc.nexpStages))
			}
		})
	}

	{
		// this one panics with a typed error and needs to be tested separately from the above (PanicsWithError())
		t.Run("iso-nopkgs", func(t *testing.T) {
			assert := assert.New(t)
			config := main.ManifestConfig(*userConfig)
			config.ImgType = "iso"
			manifest, err := main.Manifest(&config)
			assert.NoError(err) // this isn't the error we're testing for

			expError := "package \"kernel\" not found in the PackageSpec list"
			assert.PanicsWithError(expError, func() {
				_, err := manifest.Serialize(nil, isoContainers, nil)
				assert.NoError(err)
			})
		})
	}
}

// simplified representation of a manifest
type testManifest struct {
	Pipelines []pipeline `json:"pipelines"`
}
type pipeline struct {
	Name   string  `json:"name"`
	Stages []stage `json:"stages"`
}
type stage struct {
	Type string `json:"type"`
}

func checkStages(serialized manifest.OSBuildManifest, pipelineStages map[string][]string, missingStages map[string][]string) error {
	mf := &testManifest{}
	if err := json.Unmarshal(serialized, mf); err != nil {
		return err
	}
	pipelineMap := map[string]pipeline{}
	for _, pl := range mf.Pipelines {
		pipelineMap[pl.Name] = pl
	}

	for plname, stages := range pipelineStages {
		pl, found := pipelineMap[plname]
		if !found {
			return fmt.Errorf("pipeline %q not found", plname)
		}

		stageMap := map[string]bool{}
		for _, stage := range pl.Stages {
			stageMap[stage.Type] = true
		}
		for _, stage := range stages {
			if _, found := stageMap[stage]; !found {
				return fmt.Errorf("pipeline %q - stage %q - not found", plname, stage)
			}
		}
	}

	for plname, stages := range missingStages {
		pl, found := pipelineMap[plname]
		if !found {
			return fmt.Errorf("pipeline %q not found", plname)
		}

		stageMap := map[string]bool{}
		for _, stage := range pl.Stages {
			stageMap[stage.Type] = true
		}
		for _, stage := range stages {
			if _, found := stageMap[stage]; found {
				return fmt.Errorf("pipeline %q - stage %q - found (but should not be)", plname, stage)
			}
		}
	}

	return nil
}
