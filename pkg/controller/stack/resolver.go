package stack

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/go-github/v29/github"
	kabanerov1alpha2 "github.com/kabanero-io/kabanero-operator/pkg/apis/kabanero/v1alpha2"
	cutils "github.com/kabanero-io/kabanero-operator/pkg/controller/utils"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveIndex returns a structure representation of the yaml file represented by the index.
func ResolveIndex(c client.Client, repoConf kabanerov1alpha2.RepositoryConfig, namespace string, pipelines []Pipelines, triggers []Trigger, imagePrefix string) (*Index, error) {
	var indexBytes []byte

	switch {
	// GIT:
	case isGitReleaseUsable(repoConf.GitRelease):
		bytes, err := getStackIndexUsingGit(c, repoConf.GitRelease, namespace)
		if err != nil {
			return nil, err
		}
		indexBytes = bytes
	// HTTPS:
	case len(repoConf.Https.Url) != 0:
		bytes, err := getStackIndexUsingHttp(repoConf)
		if err != nil {
			return nil, err
		}
		indexBytes = bytes
	// NOT SUPPORTED:
	default:
		return nil, fmt.Errorf("No information was provided to retrieve the stack's index file from the repository identified as %v. Specify a stack repository that includes a HTTP URL location or GitHub release information.", repoConf.Name)
	}

	var index Index
	err := yaml.Unmarshal(indexBytes, &index)
	if err != nil {
		return nil, err
	}

	processIndexPostRead(&index, pipelines, triggers)

	return &index, nil
}

// Updates the loaded stack index structure for compliance with the current implementation.
func processIndexPostRead(index *Index, pipelines []Pipelines, triggers []Trigger) error {
	// Add common pipelines and image.

	tmpstack := index.Stacks[:0]
	for _, stack := range index.Stacks {
		// Stack index.yaml files may not define pipeline formation. Therefore, the following order of
		// preference is applied when obtaining pipeline information:
		// a. k.Spec.Stacks.Repositories.Pipelines.
		// b. k.Spec.Stacks.Pipelines.
		// c. index.Stack.Pipelines.
		// Note: The caller has already processed order a and b.
		if len(pipelines) != 0 {
			stack.Pipelines = pipelines
		}

		// Do not index a malformed stack that has no Image or at least one Images[].Image
		// If there is a singleton Image, assign it to the Images list
		if len(stack.Images) == 0 {
			if len(stack.Image) == 0 {
				log.Info(fmt.Sprintf("Stack %v %v not created. Index entry must contain at least one Image or Images[].", stack.Name, stack.Version))
			} else {
				stack.Images = []Images{{Id: stack.Name, Image: stack.Image}}
				tmpstack = append(tmpstack, stack)
			}
		} else {
			var imagefound bool
			imagefound = false
			for _, image := range stack.Images {
				if len(image.Image) != 0 {
					imagefound = true
				}
			}
			if imagefound {
				tmpstack = append(tmpstack, stack)
			} else {
				log.Info(fmt.Sprintf("Stack %v %v not created. No Images[].Image found.", stack.Name, stack.Version))
			}

		}
	}
	index.Stacks = tmpstack

	// Add common triggers.
	if len(index.Triggers) == 0 {
		index.Triggers = triggers
	}

	return nil
}

// SearchStack returns all stacks in the index matching the given name.
func SearchStack(stackName string, index *Index) ([]Stack, error) {
	//Locate the desired stack in the index
	var stackRefs []Stack

	for _, stackRef := range index.Stacks {
		if stackRef.Id == stackName {
			stackRefs = append(stackRefs, stackRef)
		}
	}

	if len(stackRefs) == 0 {
		//The stack referenced in the Stack resource has no match in the index
		return nil, nil
	}

	return stackRefs, nil
}

// Returns true if the user specified all values in Kabanero.Spec.Stacks.Repositories.GitRelease.
func isGitReleaseUsable(gitRelease kabanerov1alpha2.GitReleaseSpec) bool {
	return len(gitRelease.Hostname) != 0 && len(gitRelease.Organization) != 0 && len(gitRelease.Project) != 0 &&
		len(gitRelease.Release) != 0 && len(gitRelease.AssetName) != 0
}

// Retrieves a stack index file content using HTTP.
func getStackIndexUsingHttp(repoConf kabanerov1alpha2.RepositoryConfig) ([]byte, error) {
	url := repoConf.Https.Url

	// user may specify url to yaml file or directory
	matched, err := regexp.MatchString(`/([^/]+)[.]yaml$`, url)
	if err != nil {
		return nil, err
	}
	if !matched {
		url = url + "/index.yaml"
	}

	return getFromCache(url, repoConf.Https.SkipCertVerification)
}

// Retrieves a stack index file content using GitHub APIs
func getStackIndexUsingGit(c client.Client, gitRelease kabanerov1alpha2.GitReleaseSpec, namespace string) ([]byte, error) {
	var indexBytes []byte

	// Get a Github client.
	gclient, err := getGitClient(c, gitRelease, namespace)
	if err != nil {
		return nil, err
	}

	// Get the release tagged in Github as repoConf.GitRelease.Release.
	release, response, err := gclient.Repositories.GetReleaseByTag(context.Background(), gitRelease.Organization, gitRelease.Project, gitRelease.Release)
	if err != nil || response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Unable to retrieve object representing Github repository release %v. Configured GitRelease data: %v. Error: %v", gitRelease.Release, gitRelease, err)
	}
	assets := release.Assets

	// Find the asset identified as repoConf.GitRelease.AssetName and download it.
	for _, asset := range assets {
		if asset.GetName() == gitRelease.AssetName {
			id := asset.GetID()
			reader, _, err := gclient.Repositories.DownloadReleaseAsset(context.Background(), gitRelease.Organization, gitRelease.Project, id, http.DefaultClient)
			if err != nil {
				return nil, fmt.Errorf("Unable to download release asset %v. Configured GitRelease data: %v. Error: %v", gitRelease.AssetName, gitRelease, err)
			}
			defer reader.Close()

			indexBytes, err = ioutil.ReadAll(reader)
			if err != nil {
				return nil, fmt.Errorf(fmt.Sprintf("Unable to read downloaded asset %v from request. Configured GitRelease data: %v. Error: %v", gitRelease.AssetName, gitRelease, err))
			}

			break
		}
	}

	return indexBytes, err
}

// Retrieves a Git client.
func getGitClient(c client.Client, gitRelease kabanerov1alpha2.GitReleaseSpec, namespace string) (*github.Client, error) {
	var client *github.Client

	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: gitRelease.SkipCertVerification}}

	// Search all secrets under the given namespace for the one containing the required hostname.
	secret, err := cutils.GetMatchingSecret(c, namespace, secretFilter, gitRelease.Hostname)
	if err != nil {
		return nil, err
	}

	var pat []byte
	if secret != nil {
		pat, _ = secret.Data["password"]
	}

	httpClient, err := cutils.GetHTTPClient(pat, transport)
	if err != nil {
		return nil, err
	}

	switch {
	// GHE.
	case gitRelease.Hostname != "github.com":
		// GHE hostnames must be suffixed with /api/v3/ otherwise 406 status codes
		// will be returned. Using NewEnterpriseClient will do that for us automatically.
		url := "https://" + gitRelease.Hostname
		eclient, err := github.NewEnterpriseClient(url, url, httpClient)
		if err != nil {
			return nil, err
		}
		client = eclient
	// Non GHE.
	default:
		client = github.NewClient(httpClient)
	}

	return client, nil
}

// Custom filter method that allows the retrieval of a secret containing an annotation with a value
// that has the input filter strings (hostname). If there are multiple matching secrets, the annotation
// with the lexically lowest value key (kabanero.io/git-*) is used. If no secret matches annotation key
// kabanero.io/git-*, but there are secrets with an annotation value that matches the hostname, the
// first one seen is used. If a secret could not be found, nil is returned.
func secretFilter(secretList *corev1.SecretList, filterStrings ...string) (*corev1.Secret, error) {
	var keyMatchingSecret *corev1.Secret = nil
	var noKeyMatchingSecret *corev1.Secret = nil
	kabKey := ""
	for _, secret := range secretList.Items {
		annotations := secret.GetAnnotations()
		for key, value := range annotations {
			matchedUrl, err := regexp.MatchString("^https?://", value)
			if err != nil {
				return nil, err
			}
			if matchedUrl {
				surl, err := url.Parse(value)
				if err != nil {
					fmt.Println("Unable to parse secret annotation value URL: ", surl, ". Secret: ", secret)
				}

				if surl.Hostname() == filterStrings[0] {
					if strings.HasPrefix(key, "kabanero.io/git-") {
						if len(kabKey) == 0 {
							kabKey = key
							keyMatchingSecret = secret.DeepCopy()
						} else {
							if kabKey > key {
								kabKey = key
								keyMatchingSecret = secret.DeepCopy()
							}
						}
					} else {
						// Save the first secret we see matching the hostname.
						if noKeyMatchingSecret == nil {
							noKeyMatchingSecret = secret.DeepCopy()
						}
					}
				}
			}
		}
	}

	if keyMatchingSecret != nil {
		return keyMatchingSecret, nil
	}
	if noKeyMatchingSecret != nil {
		return noKeyMatchingSecret, nil
	}

	return nil, nil
}
