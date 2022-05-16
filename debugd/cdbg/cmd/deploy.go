package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"

	"github.com/edgelesssys/constellation/debugd/cdbg/config"
	"github.com/edgelesssys/constellation/debugd/cdbg/state"
	"github.com/edgelesssys/constellation/debugd/coordinator"
	"github.com/edgelesssys/constellation/debugd/debugd"
	depl "github.com/edgelesssys/constellation/debugd/debugd/deploy"
	pb "github.com/edgelesssys/constellation/debugd/service"
	configc "github.com/edgelesssys/constellation/internal/config"
	"github.com/edgelesssys/constellation/internal/constants"
	"github.com/edgelesssys/constellation/internal/deploy/ssh"
	"github.com/edgelesssys/constellation/internal/file"
	statec "github.com/edgelesssys/constellation/internal/state"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploys a self-compiled coordinator binary and SSH keys on the current constellation",
	Long: `Deploys a self-compiled coordinator binary and SSH keys on the current constellation.
Uses config provided by --config and reads constellation config from its default location.
If required, you can override the IP addresses that are used for a deployment by specifying "--ips" and a list of IP addresses.
Specifying --coordinator will upload the coordinator from the specified path.`,
	RunE:    runDeploy,
	Example: "cdbg deploy\ncdbg deploy --config /path/to/config\ncdbg deploy --coordinator /path/to/coordinator --ips 192.0.2.1,192.0.2.2,192.0.2.3 --config /path/to/config",
}

func runDeploy(cmd *cobra.Command, args []string) error {
	configName, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}
	fileHandler := file.NewHandler(afero.NewOsFs())
	debugConfig, err := config.FromFile(fileHandler, configName)
	if err != nil {
		return err
	}
	constellationConfig, err := configc.FromFile(fileHandler, constants.ConfigFilename)
	if err != nil {
		return err
	}

	return deploy(cmd, fileHandler, constellationConfig, debugConfig, coordinator.NewFileStreamer(afero.NewOsFs()))
}

func deploy(cmd *cobra.Command, fileHandler file.Handler, constellationConfig *configc.Config, debugConfig *config.CDBGConfig, reader fileToStreamReader) error {
	overrideCoordinatorPath, err := cmd.Flags().GetString("coordinator")
	if err != nil {
		return err
	}
	if len(overrideCoordinatorPath) > 0 {
		debugConfig.ConstellationDebugConfig.CoordinatorPath = overrideCoordinatorPath
	}

	overrideIPs, err := cmd.Flags().GetStringSlice("ips")
	if err != nil {
		return err
	}
	var ips []string
	if len(overrideIPs) > 0 {
		ips = overrideIPs
	} else {
		var stat statec.ConstellationState
		err := fileHandler.ReadJSON(constants.StateFilename, &stat)
		if errors.Is(err, fs.ErrNotExist) {
			log.Println("Unable to load statefile. Maybe you forgot to run \"constellation create ...\" first?")
			return fmt.Errorf("loading statefile failed: %w", err)
		} else if err != nil {
			return fmt.Errorf("loading statefile failed: %w", err)
		}
		ips, err = getIPsFromConfig(stat, *constellationConfig)
		if err != nil {
			return err
		}
	}

	for _, ip := range ips {
		input := deployOnEndpointInput{
			debugdEndpoint:  net.JoinHostPort(ip, debugd.DebugdPort),
			coordinatorPath: debugConfig.ConstellationDebugConfig.CoordinatorPath,
			reader:          reader,
			authorizedKeys:  debugConfig.ConstellationDebugConfig.AuthorizedKeys,
			systemdUnits:    debugConfig.ConstellationDebugConfig.SystemdUnits,
		}
		if err := deployOnEndpoint(cmd.Context(), input); err != nil {
			return err
		}
	}

	return nil
}

type deployOnEndpointInput struct {
	debugdEndpoint  string
	coordinatorPath string
	reader          fileToStreamReader
	authorizedKeys  []ssh.UserKey
	systemdUnits    []depl.SystemdUnit
}

// deployOnEndpoint deploys SSH public keys, systemd units and a locally built coordinator binary to a debugd endpoint.
func deployOnEndpoint(ctx context.Context, in deployOnEndpointInput) error {
	log.Printf("Deploying on %v\n", in.debugdEndpoint)
	dialCTX, cancel := context.WithTimeout(ctx, debugd.GRPCTimeout)
	defer cancel()
	conn, err := grpc.DialContext(dialCTX, in.debugdEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("error connecting to other instance via gRPC: %w", err)
	}
	defer conn.Close()
	client := pb.NewDebugdClient(conn)

	log.Println("Uploading authorized keys")
	pbKeys := []*pb.AuthorizedKey{}
	for _, key := range in.authorizedKeys {
		pbKeys = append(pbKeys, &pb.AuthorizedKey{
			Username: key.Username,
			KeyValue: key.PublicKey,
		})
	}
	authorizedKeysResponse, err := client.UploadAuthorizedKeys(ctx, &pb.UploadAuthorizedKeysRequest{Keys: pbKeys}, grpc.WaitForReady(true))
	if err != nil || authorizedKeysResponse.Status != pb.UploadAuthorizedKeysStatus_UPLOAD_AUTHORIZED_KEYS_SUCCESS {
		return fmt.Errorf("uploading coordinator to instance %v failed: %v / %w", in.debugdEndpoint, authorizedKeysResponse, err)
	}

	if len(in.systemdUnits) > 0 {
		log.Println("Uploading systemd unit files")

		pbUnits := []*pb.ServiceUnit{}
		for _, unit := range in.systemdUnits {
			pbUnits = append(pbUnits, &pb.ServiceUnit{
				Name:     unit.Name,
				Contents: unit.Contents,
			})
		}
		uploadSystemdServiceUnitsResponse, err := client.UploadSystemServiceUnits(ctx, &pb.UploadSystemdServiceUnitsRequest{Units: pbUnits})
		if err != nil || uploadSystemdServiceUnitsResponse.Status != pb.UploadSystemdServiceUnitsStatus_UPLOAD_SYSTEMD_SERVICE_UNITS_SUCCESS {
			return fmt.Errorf("uploading systemd service unit to instance %v failed: %v / %w", in.debugdEndpoint, uploadSystemdServiceUnitsResponse, err)
		}
	}

	stream, err := client.UploadCoordinator(ctx)
	if err != nil {
		return fmt.Errorf("starting coordinator upload to instance %v failed: %w", in.debugdEndpoint, err)
	}
	streamErr := in.reader.ReadStream(in.coordinatorPath, stream, debugd.Chunksize, true)

	uploadResponse, closeErr := stream.CloseAndRecv()
	if closeErr != nil {
		return fmt.Errorf("closing upload stream after uploading coordinator to %v failed: %w", in.debugdEndpoint, closeErr)
	}
	if uploadResponse.Status == pb.UploadCoordinatorStatus_UPLOAD_COORDINATOR_FILE_EXISTS {
		log.Println("Coordinator was already uploaded")
		return nil
	}
	if uploadResponse.Status != pb.UploadCoordinatorStatus_UPLOAD_COORDINATOR_SUCCESS || streamErr != nil {
		return fmt.Errorf("uploading coordinator to instance %v failed: %v / %w", in.debugdEndpoint, uploadResponse, streamErr)
	}
	log.Println("Uploaded coordinator")
	return nil
}

func getIPsFromConfig(stat statec.ConstellationState, config configc.Config) ([]string, error) {
	coordinators, nodes, err := state.GetScalingGroupsFromConfig(stat, &config)
	if err != nil {
		return nil, err
	}
	return append(coordinators.PublicIPs(), nodes.PublicIPs()...), nil
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().StringSlice("ips", nil, "override the ips that the coordinator will be uploaded to (defaults to ips from constellation config)")
	deployCmd.Flags().String("coordinator", "", "override the path to the coordinator binary uploaded to instances (defaults to path set in config)")
}

type fileToStreamReader interface {
	ReadStream(filename string, stream coordinator.WriteChunkStream, chunksize uint, showProgress bool) error
}
