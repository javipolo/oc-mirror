package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/openshift/oc-mirror/v2/internal/pkg/api/v2alpha1"
	"github.com/openshift/oc-mirror/v2/internal/pkg/emoji"
)

func (o *ExecutorSchema) DryRun(ctx context.Context, allImages []v2alpha1.CopyImageSchema) error {
	// set up location of logs dir
	outDir := filepath.Join(o.Opts.Global.WorkingDir, dryRunOutDir)
	// clean up logs directory
	os.RemoveAll(outDir)

	// create logs directory
	err := o.MakeDir.makeDirAll(outDir, 0755)
	if err != nil {
		o.Log.Error(" %v ", err)
		return err
	}
	// creating file for storing list of cached images
	mappingTxtFilePath := filepath.Join(outDir, mappingFile)
	mappingTxtFile, err := os.Create(mappingTxtFilePath)
	if err != nil {
		return err
	}
	defer mappingTxtFile.Close()
	imagesAvailable := map[string]bool{}
	nbMissingImgs := 0
	var buff bytes.Buffer
	var missingImgsBuff bytes.Buffer
	for _, img := range allImages {
		buff.WriteString(img.Source + "=" + img.Destination + "\n")
		if o.Opts.IsMirrorToDisk() {
			exists, err := o.Mirror.Check(ctx, img.Destination, o.Opts, false)
			if err != nil {
				o.Log.Debug("unable to check existence of %s in local cache: %v", img.Destination, err)
			}
			if err != nil || !exists {
				missingImgsBuff.WriteString(img.Source + "=" + img.Destination + "\n")
				nbMissingImgs++
			}
		}
	}

	_, err = mappingTxtFile.Write(buff.Bytes())
	if err != nil {
		return err
	}
	if nbMissingImgs > 0 {
		// creating file for storing list of cached images
		missingImgsFilePath := filepath.Join(outDir, missingImgsFile)
		missingImgsTxtFile, err := os.Create(missingImgsFilePath)
		if err != nil {
			return err
		}
		defer missingImgsTxtFile.Close()
		_, err = missingImgsTxtFile.Write(missingImgsBuff.Bytes())
		if err != nil {
			return err
		}
		o.Log.Warn(emoji.Warning+"  %d/%d images necessary for mirroring are not available in the cache.", nbMissingImgs, len(allImages))
		o.Log.Warn("List of missing images in : %s.\nplease re-run the mirror to disk process", missingImgsFilePath)
	}

	if len(imagesAvailable) > 0 {
		o.Log.Info("all %d images required for mirroring are available in local cache. You may proceed with mirroring from disk to disconnected registry", len(imagesAvailable))
	}
	o.Log.Info(emoji.PageFacingUp+" list of all images for mirroring in : %s", mappingTxtFilePath)
	return nil
}

func (o *ExecutorSchema) ClusterResourcesOnly(ctx context.Context, allImages []v2alpha1.CopyImageSchema) error {
	if len(allImages) == 0 {
		o.Log.Info(emoji.PageFacingUp + " No images collected. Skipping cluster resources generation.")
		return nil
	}

	o.Log.Info(emoji.PageFacingUp + " Generating cluster resources without mirroring images...")

	// create cluster-resources directory and clean it (similar to the existing setupWorkingDir logic)
	clusterResourcesPath := filepath.Join(o.Opts.Global.WorkingDir, clusterResourcesDir)
	o.Log.Trace("creating cluster-resources directory %s", clusterResourcesPath)

	if err := os.RemoveAll(clusterResourcesPath); err != nil {
		o.Log.Error("failed to clear folder %s: %v", clusterResourcesPath, err)
		return err
	}

	err := o.MakeDir.makeDirAll(clusterResourcesPath, 0755)
	if err != nil {
		o.Log.Error("setupWorkingDir for cluster resources %v", err)
		return err
	}

	// Generate cluster resources using the collected images (assuming they would all be successfully copied)
	forceRepositoryScope := o.Opts.Global.MaxNestedPaths > 0

	// create IDMS/ITMS
	if err := o.ClusterResources.IDMS_ITMSGenerator(allImages, forceRepositoryScope); err != nil {
		return err
	}

	// create catalog source
	if err := o.ClusterResources.CatalogSourceGenerator(allImages); err != nil {
		return err
	}

	// create cluster catalog
	if err := o.ClusterResources.ClusterCatalogGenerator(allImages); err != nil {
		return err
	}

	// generate signature config map
	if err := o.ClusterResources.GenerateSignatureConfigMap(allImages); err != nil {
		// as this is not a seriously fatal error we just log the error
		o.Log.Warn("%s", err)
	}

	// create updateService if platform.graph is enabled
	if o.Config.Mirror.Platform.Graph {
		graphImage, err := o.Release.GraphImage()
		if err != nil {
			return err
		}
		releaseImage, err := o.Release.ReleaseImage(ctx)
		if err != nil {
			return err
		}
		if err := o.ClusterResources.UpdateServiceGenerator(graphImage, releaseImage); err != nil {
			return err
		}
	}

	o.Log.Info(emoji.PageFacingUp+" Cluster resources generated in: %s", clusterResourcesPath)
	return nil
}
