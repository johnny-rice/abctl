package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"

	"github.com/airbytehq/abctl/internal/abctl"
	"github.com/airbytehq/abctl/internal/paths"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pterm/pterm"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Version contains al the version information that is being tracked.
type Version struct {
	// Version is the platform version
	Version string
	// Arch is the platform architecture
	Arch string
	// Platform is the platform name
	Platform string
}

// Client interface for testing purposes. Includes only the methods used by the underlying docker package.
type Client interface {
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerRemove(ctx context.Context, container string, options container.RemoveOptions) error
	ContainerStart(ctx context.Context, container string, options container.StartOptions) error
	ContainerStop(ctx context.Context, container string, options container.StopOptions) error
	CopyFromContainer(ctx context.Context, container, srcPath string) (io.ReadCloser, container.PathStat, error)

	ContainerExecCreate(ctx context.Context, container string, config container.ExecOptions) (types.IDResponse, error)
	ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error)
	ContainerExecStart(ctx context.Context, execID string, config container.ExecStartOptions) error

	ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	ImageSave(ctx context.Context, imageIDs []string) (io.ReadCloser, error)

	ServerVersion(ctx context.Context) (types.Version, error)
	VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error)
	Info(ctx context.Context) (system.Info, error)
}

var _ Client = (*client.Client)(nil)

// Docker for handling communication with the docker processes.
// Can be created with default settings by calling New or with a custom Client by manually instantiating this type.
type Docker struct {
	Client Client
}

// New returns a new Docker type with a default Client implementation.
func New(ctx context.Context) (*Docker, error) {
	// convert the client.NewClientWithOpts to a newPing function
	f := func(opts ...client.Opt) (pinger, error) {
		var p pinger
		var err error
		p, err = client.NewClientWithOpts(opts...)
		if err != nil {
			return nil, err
		}
		return p, nil
	}

	return newWithOptions(ctx, f, runtime.GOOS)
}

// newPing exists for testing purposes.
// This allows a mock docker client (client.Client) to be injected for tests
type newPing func(...client.Opt) (pinger, error)

// pinger interface for testing purposes.
// Adds the Ping method to the Client interface which is used by the New function.
type pinger interface {
	Client
	Ping(ctx context.Context) (types.Ping, error)
}

var _ pinger = (*client.Client)(nil)

// newWithOptions allows for the docker client to be injected for testing purposes.
func newWithOptions(ctx context.Context, newPing newPing, goos string) (*Docker, error) {

	var potentialHosts []string

	// The best guess at the docker host comes from the "docker context inspect" command,
	// which describes the current context in detail.
	if out, err := exec.Command("docker", "context", "inspect").Output(); err == nil {
		var data []struct {
			Endpoints struct {
				Docker struct {
					Host string
				} `json:"docker"`
			}
		}
		if err := json.Unmarshal(out, &data); err == nil {
			if len(data) > 0 && data[0].Endpoints.Docker.Host != "" {
				potentialHosts = append(potentialHosts, data[0].Endpoints.Docker.Host)
			}
		}
	}

	// If the code above fails, then fall back to some educated guesses.
	// Unfortunately, these can easily be wrong if the user is using a non-standard
	// docker context, or if we've missed any common installation configs here.
	switch goos {
	case "darwin":
		potentialHosts = append(potentialHosts,
			"unix:///var/run/docker.sock",
			fmt.Sprintf("unix://%s/.docker/run/docker.sock", paths.UserHome),
		)
	case "windows":
		potentialHosts = append(potentialHosts, "npipe:////./pipe/docker_engine")
	default:
		potentialHosts = append(potentialHosts,
			"unix:///var/run/docker.sock",
			fmt.Sprintf("unix://%s/.docker/desktop/docker-cli.sock", paths.UserHome),
		)
	}

	// Do not sample Docker traces. Dockers Net/HTTP client has Otel instrumentation enabled.
	// URL's and other fields may contain PII, or sensitive information.
	noopTraceProvider := trace.NewTracerProvider(
		trace.WithSampler(trace.NeverSample()),
	)

	dockerOpts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation(), client.WithTraceProvider(noopTraceProvider)}

	for _, host := range potentialHosts {
		dockerCli, err := createAndPing(ctx, newPing, host, dockerOpts)
		if err != nil {
			pterm.Debug.Printfln("error connecting to docker host %s: %s", host, err)
		} else {
			return &Docker{Client: dockerCli}, nil
		}
	}

	return nil, fmt.Errorf("%w: unable to create docker client", abctl.ErrDocker)
}

// createAndPing attempts to create a docker client and ping it to ensure we can communicate
func createAndPing(ctx context.Context, newPing newPing, host string, opts []client.Opt) (Client, error) {
	// Pass client.WithHost first to ensure it runs prior to the client.FromEnv call.
	// We want the DOCKER_HOST to be used if it has been specified, overriding our host.
	cli, err := newPing(append([]client.Opt{client.WithHost(host)}, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("unable to create docker client: %w", err)
	}

	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("unable to ping docker client: %w", err)
	}

	return cli, nil
}

// Version returns the version information from the underlying docker process.
func (d *Docker) Version(ctx context.Context) (Version, error) {
	ver, err := d.Client.ServerVersion(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("unable to determine server version: %w", err)
	}

	return Version{
		Version:  ver.Version,
		Arch:     ver.Arch,
		Platform: ver.Platform.Name,
	}, nil
}
