package generic

import (
	"errors"
	"github.com/jfrog/jfrog-cli-go/jfrog-cli/artifactory/spec"
	"github.com/jfrog/jfrog-cli-go/jfrog-cli/artifactory/utils"
	"github.com/jfrog/jfrog-cli-go/jfrog-cli/utils/config"
	"github.com/jfrog/jfrog-client-go/artifactory"
	"github.com/jfrog/jfrog-client-go/artifactory/buildinfo"
	"github.com/jfrog/jfrog-client-go/artifactory/services"
	clientutils "github.com/jfrog/jfrog-client-go/artifactory/services/utils"
	"github.com/jfrog/jfrog-client-go/utils/errorutils"
	"github.com/jfrog/jfrog-client-go/utils/log"
	"os"
	"strconv"
	"strings"
)

// Uploads the artifacts in the specified local path pattern to the specified target path.
// Returns the total number of artifacts successfully uploaded.
func Upload(uploadSpec *spec.SpecFiles, configuration *UploadConfiguration) (successCount, failCount int, err error) {

	// Create Service Manager:
	certPath, err := utils.GetJfrogSecurityDir()
	if err != nil {
		return 0, 0, err
	}
	minChecksumDeploySize, err := getMinChecksumDeploySize()
	if err != nil {
		return 0, 0, err
	}
	servicesConfig, err := createUploadServiceConfig(configuration.ArtDetails, configuration, certPath, minChecksumDeploySize)
	if err != nil {
		return 0, 0, err
	}
	servicesManager, err := artifactory.New(servicesConfig)
	if err != nil {
		return 0, 0, err
	}

	// Build Info Collection:
	isCollectBuildInfo := len(configuration.BuildName) > 0 && len(configuration.BuildNumber) > 0
	if isCollectBuildInfo && !configuration.DryRun {
		if err := utils.SaveBuildGeneralDetails(configuration.BuildName, configuration.BuildNumber); err != nil {
			return 0, 0, err
		}
		for i := 0; i < len(uploadSpec.Files); i++ {
			addBuildProps(&uploadSpec.Get(i).Props, configuration.BuildName, configuration.BuildNumber)
		}
	}

	// Upload Loop:
	var filesInfo []clientutils.FileInfo
	var errorOccurred = false
	for i := 0; i < len(uploadSpec.Files); i++ {

		uploadParams, err := getUploadParams(uploadSpec.Get(i), configuration)
		if err != nil {
			errorOccurred = true
			log.Error(err)
			continue
		}

		artifacts, uploaded, failed, err := servicesManager.UploadFiles(uploadParams)

		filesInfo = append(filesInfo, artifacts...)
		failCount += failed
		successCount += uploaded
		if err != nil {
			errorOccurred = true
			log.Error(err)
			continue
		}
	}

	if errorOccurred {
		err = errors.New("Upload finished with errors. Please review the logs")
		return
	}
	if failCount > 0 {
		return
	}

	// Build Info
	if isCollectBuildInfo && !configuration.DryRun {
		buildArtifacts := convertFileInfoToBuildArtifacts(filesInfo)
		populateFunc := func(partial *buildinfo.Partial) {
			partial.Artifacts = buildArtifacts
		}
		err = utils.SavePartialBuildInfo(configuration.BuildName, configuration.BuildNumber, populateFunc)
	}
	return
}

func convertFileInfoToBuildArtifacts(filesInfo []clientutils.FileInfo) []buildinfo.Artifact {
	buildArtifacts := make([]buildinfo.Artifact, len(filesInfo))
	for i, fileInfo := range filesInfo {
		buildArtifacts[i] = fileInfo.ToBuildArtifacts()
	}
	return buildArtifacts
}

func createUploadServiceConfig(artDetails *config.ArtifactoryDetails, flags *UploadConfiguration, certPath string, minChecksumDeploySize int64) (artifactory.Config, error) {
	artAuth, err := artDetails.CreateArtAuthConfig()
	if err != nil {
		return nil, err
	}
	servicesConfig, err := artifactory.NewConfigBuilder().
		SetArtDetails(artAuth).
		SetDryRun(flags.DryRun).
		SetCertificatesPath(certPath).
		SetMinChecksumDeploy(minChecksumDeploySize).
		SetThreads(flags.Threads).
		SetLogger(log.Logger).
		Build()
	return servicesConfig, err
}

func getMinChecksumDeploySize() (int64, error) {
	minChecksumDeploySize := os.Getenv("JFROG_CLI_MIN_CHECKSUM_DEPLOY_SIZE_KB")
	if minChecksumDeploySize == "" {
		return 10240, nil
	}
	minSize, err := strconv.ParseInt(minChecksumDeploySize, 10, 64)
	err = errorutils.CheckError(err)
	if err != nil {
		return 0, err
	}
	return minSize * 1000, nil
}

func addBuildProps(props *string, buildName, buildNumber string) error {
	if buildName == "" || buildNumber == "" {
		return nil
	}
	buildProps, err := utils.CreateBuildProperties(buildName, buildNumber)
	if err != nil {
		return err
	}

	if len(*props) > 0 && !strings.HasSuffix(*props, ";") && len(buildProps) > 0 {
		*props += ";"
	}
	*props += buildProps
	return nil
}

type UploadConfiguration struct {
	Deb                   string
	Threads               int
	MinChecksumDeploySize int64
	BuildName             string
	BuildNumber           string
	DryRun                bool
	Symlink               bool
	ExplodeArchive        bool
	ArtDetails            *config.ArtifactoryDetails
	Retries               int
}

func getUploadParams(f *spec.File, configuration *UploadConfiguration) (uploadParams services.UploadParams, err error) {
	uploadParams = services.NewUploadParams()
	uploadParams.ArtifactoryCommonParams = f.ToArtifactoryCommonParams()
	uploadParams.Recursive, err = f.IsRecursive(true)
	if err != nil {
		return
	}

	uploadParams.Regexp, err = f.IsRegexp(false)
	if err != nil {
		return
	}

	uploadParams.IncludeDirs, err = f.IsIncludeDirs(false)
	if err != nil {
		return
	}

	uploadParams.Flat, err = f.IsFlat(true)
	if err != nil {
		return
	}

	uploadParams.ExplodeArchive, err = f.IsExplode(false)
	if err != nil {
		return
	}

	uploadParams.Deb = configuration.Deb
	uploadParams.Symlink = configuration.Symlink
	uploadParams.Retries = configuration.Retries
	return
}
