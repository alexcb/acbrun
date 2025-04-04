package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/alexcb/acbrun/v2"
	"github.com/jessevdk/go-flags"
	"github.com/opencontainers/go-digest"
	imagespec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/tidwall/sjson"
)

//go:embed config.json
var configJSONTemplate string

var opts struct {
	// Slice of bool will append 'true' each time the option
	// is encountered (can be set multiple times, like -vvv)
	Verbose      []bool `short:"v" long:"verbose" description:"Show verbose debug information"`
	Keep         bool   `long:"keep" description:"Keep temporary working directory"`
	HostNetwork  bool   `long:"host-network" description:"Allow host network access"`
	BindLocalDir bool   `long:"bind-local-dir" description:"Bind current working directory to /local-dir"`
	Reentrant    bool   `long:"reentrant" description:"Keep container filesystem intact and allow multiple or concurrent runs"`
	Interactive  bool   `long:"interactive" description:"pass through stdin"`
	Output       string `long:"output" description:"Output image after execution"`
	Name         string `long:"name" description:"Container name"`
}

type Manifest struct {
	Config   string   `json:"Config,omitempty"`
	RepoTags []string `json:"RepoTags,omitempty"`
	Layers   []string `json:"Layers,omitempty"`
}

func getLayers(manifestPath string) ([]string, error) {
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return nil, err
	}
	defer manifestFile.Close()
	manifestData, err := ioutil.ReadAll(manifestFile)
	if err != nil {
		return nil, err
	}

	var result []Manifest
	err = json.Unmarshal([]byte(manifestData), &result)
	if err != nil {
		return nil, err
	}
	if len(result) != 1 {
		panic("expected 1 result")
	}
	return result[0].Layers, nil
}

func isVerbose(verbose []bool) bool {
	return len(verbose) > 0
}

func main() {

	args, err := flags.ParseArgs(&opts, os.Args)
	if err != nil {
		panic(err)
	}
	verbose := isVerbose(opts.Verbose)
	progName := "acbrun"
	if len(args) > 0 {
		progName = args[0]
	}
	if len(args) != 4 {
		fmt.Fprintf(os.Stderr, "usage: %s <image.tar.gz> <sha256sum> <container name> <command>\n", progName)
		os.Exit(1)
	}
	image := args[1]
	expectedImageSha256Sum := args[2]
	command := args[3]

	containerName := opts.Name
	if containerName == "" {
		if opts.Reentrant {
			fmt.Fprintf(os.Stderr, "error: the --reentrant mode requires a --name value\n")
			os.Exit(1)
		}
		containerName = acbrun.RandStringBytesMask(12)
		if verbose {
			fmt.Fprintf(os.Stderr, "using random container name %s\n", containerName)
		}
	}

	var workingDir string
	var needsCreation bool
	if opts.Reentrant {
		workingDir = filepath.Join("/tmp", "acbrun-"+containerName)
		_, err := os.Stat(workingDir)
		if err != nil {
			if os.IsNotExist(err) {
				needsCreation = true
			} else {
				panic(err)
			}
		}
		if verbose {
			if needsCreation {
				fmt.Fprintf(os.Stderr, "reentrant mode did not find existing directory %s; it will create it\n", workingDir)
			} else {
				fmt.Fprintf(os.Stderr, "reentrant mode found existing directory %s; skipping creation step\n", workingDir)
			}
		}
		if needsCreation {
			err = os.Mkdir(workingDir, 0755)
			if err != nil {
				panic(err)
			}
		}

	} else {
		needsCreation = true
		var err error
		workingDir, err = os.MkdirTemp("", fmt.Sprintf("acbrun-%s", containerName))
		if err != nil {
			panic(err)
		}
		if opts.Keep {
			fmt.Fprintf(os.Stderr, "keeping temporary working directory: %s\n", workingDir)
		} else {
			defer os.RemoveAll(workingDir)
		}
	}

	rootFS := filepath.Join(workingDir, "rootfs")
	if needsCreation {
		actualSha256HashHexString, err := acbrun.GetTarSha256String(image)
		if err != nil {
			panic(err)
		}

		if actualSha256HashHexString != expectedImageSha256Sum {
			if expectedImageSha256Sum == "skip-sha256-validation" {
				fmt.Fprintf(os.Stderr, "WARNING: continuing due to skip-sha256-validation option (actual value is %s)\n", actualSha256HashHexString)
			} else {
				fmt.Fprintf(os.Stderr, "expected sha256 sum %s does not match actual sum of %s: %s\n", expectedImageSha256Sum, image, actualSha256HashHexString)
				os.Exit(1)
			}
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "%s sha256sum of %s validation complete\n", image, actualSha256HashHexString)
		}
		r, err := os.Open(image)
		if err != nil {
			panic(err)
		}
		defer r.Close()
		acbrun.ExtractTarGz(r, workingDir)
		layers, err := getLayers(filepath.Join(workingDir, "manifest.json"))
		if err != nil {
			panic(err)
		}
		if len(layers) == 0 {
			panic("no layer data")
		}
		if err := os.Mkdir(rootFS, 0755); err != nil {
			panic(err)
		}
		for _, layer := range layers {
			if verbose {
				fmt.Fprintf(os.Stderr, "extracting %s\n", layer)
			}
			r, err := os.Open(filepath.Join(workingDir, layer))
			if err != nil {
				panic(err)
			}
			defer r.Close()
			acbrun.ExtractTarGz(r, rootFS)
		}
	}

	configJSON := configJSONTemplate

	if opts.Reentrant {
		configJSON, err = sjson.Set(configJSON, "process.args", []string{"sh", "-c", "while true; do sleep 1; done"})
		if err != nil {
			panic(err)
		}
	} else {
		configJSON, err = sjson.Set(configJSON, "process.args", []string{"sh", "-c", command})
		if err != nil {
			panic(err)
		}
	}
	if !opts.HostNetwork {
		configJSON, err = sjson.Set(configJSON, "linux.namespaces.-1", map[string]string{"type": "network"})
		if err != nil {
			panic(err)
		}
	}

	if opts.BindLocalDir {
		actualWorkingDir, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		configJSON, err = sjson.Set(configJSON, "mounts.-1", map[string]interface{}{
			"destination": "/local-dir",
			"type":        "bind",
			"source":      actualWorkingDir,
			"options": []string{
				"rbind",
				"rprivate",
			},
		})
		if err != nil {
			panic(err)
		}
	}

	if opts.Interactive && !opts.Reentrant {
		configJSON, err = sjson.Set(configJSON, "process.terminal", true)
		if err != nil {
			panic(err)
		}
	}

	newConfigFile, err := os.Create(filepath.Join(workingDir, "config.json"))
	if err != nil {
		panic(err)
	}
	defer newConfigFile.Close()
	_, err = newConfigFile.WriteString(configJSON)
	if err != nil {
		panic(err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "running runc\n")
	}
	needsRun := true
	if opts.Reentrant {
		isRunning, err := acbrun.IsContainerRunning(containerName)
		if err != nil {
			panic(err)
		}
		needsRun = !isRunning
	}
	if needsRun {
		commandArgs := []string{"runc", "run"}
		if opts.Reentrant {
			commandArgs = append(commandArgs, "--detach")
		}
		commandArgs = append(commandArgs, containerName)
		cmd := exec.Command(commandArgs[0], commandArgs[1:]...)
		cmd.Dir = workingDir
		if !opts.Reentrant {
			// whenever runc -d is used, if stdout or stderr are specified, it causes
			// commands like "./acbrun ... | cat" to hang
			// this needs to be fixed somehow, since we need to surface errors if runc run -d fails
			// note that is also fails when we give it a bytes buffer or even a custom buffer that doesnt even print
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}

		if opts.Interactive {
			cmd.Stdin = os.Stdin
		}

		// TODO I think we need to create some sort of FILE-based stdout/stderr connection here
		// where we can completely detach it from this current process
		// or clean it up after the Run() comes back.
		// the issue might be related to the "runc --detach" process continuing to persist AFTER
		// this go process returns
		// This seems related: https://github.com/opencontainers/runc/issues/1721

		err = cmd.Run()
		if err != nil {
			panic(err)
		}
	}

	if opts.Reentrant {
		commandArgs := []string{"runc", "exec"}
		if opts.Interactive {
			commandArgs = append(commandArgs, "--tty")
		}
		commandArgs = append(commandArgs, containerName, "/bin/sh", "-c", command)
		cmd := exec.Command(commandArgs[0], commandArgs[1:]...)
		cmd.Dir = workingDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if opts.Interactive {
			cmd.Stdin = os.Stdin
		}
		err = cmd.Run()
		if err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				os.Exit(exiterr.ExitCode())
			}
			panic(err)
		}
	}

	if opts.Output == "" {
		return
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "outputing image to %s\n", opts.Output)
	}

	outputDir, err := os.MkdirTemp("", "")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(outputDir)

	rootFSPath := filepath.Join(outputDir, "rootfs.tar.gz")
	out, err := os.Create(rootFSPath)
	if err != nil {
		panic(err)
	}
	defer out.Close()

	err = acbrun.CreateTarGz(rootFS, out)
	if err != nil {
		panic(err)
	}

	outputRootFSTarGzSha256, err := acbrun.GetTarSha256String(rootFSPath)
	if err != nil {
		panic(err)
	}
	rootFSName := fmt.Sprintf("%s.tar.gz", outputRootFSTarGzSha256)
	err = os.Rename(rootFSPath, filepath.Join(outputDir, rootFSName))
	if err != nil {
		panic(err)
	}

	imageConfig := imagespec.Image{
		Platform: imagespec.Platform{
			Architecture: "amd64", // TODO
			OS:           "linux",
		},
		Config: imagespec.ImageConfig{
			Env: []string{
				"PATH=/bin:/usr/bin", // TODO
			},
		},
		RootFS: imagespec.RootFS{
			Type: "layers",
			DiffIDs: []digest.Digest{
				digest.Digest(fmt.Sprintf("sha256:%s", outputRootFSTarGzSha256)),
			},
		},
	}
	imageConfigJSON, err := json.Marshal(imageConfig)
	if err != nil {
		panic(err)
	}

	h := sha256.New()
	h.Write(imageConfigJSON)
	imageConfigJSONSha256 := hex.EncodeToString(h.Sum(nil))

	imageConfigName := fmt.Sprintf("sha256:%s", imageConfigJSONSha256)
	imageConfigJSONFile, err := os.Create(filepath.Join(outputDir, imageConfigName))
	if err != nil {
		panic(err)
	}
	defer imageConfigJSONFile.Close()
	_, err = imageConfigJSONFile.Write(imageConfigJSON)
	if err != nil {
		panic(err)
	}

	imageManifest := Manifest{
		Config: imageConfigName,
		Layers: []string{rootFSName},
	}
	imageManifestJson, err := json.Marshal([]Manifest{imageManifest})
	if err != nil {
		panic(err)
	}

	imageManifestJsonFile, err := os.Create(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		panic(err)
	}
	defer imageManifestJsonFile.Close()
	_, err = imageManifestJsonFile.Write(imageManifestJson)
	if err != nil {
		panic(err)
	}

	outputImage, err := os.Create(opts.Output)
	if err != nil {
		panic(err)
	}
	defer outputImage.Close()

	err = acbrun.CreateTarGz(outputDir, outputImage)
	if err != nil {
		panic(err)
	}

}
