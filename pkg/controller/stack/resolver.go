package stack

import (
	//"encoding/base64"
	"fmt"
	"regexp"

	"github.com/go-logr/logr"
	kabanerov1alpha2 "github.com/kabanero-io/kabanero-operator/pkg/apis/kabanero/v1alpha2"
	"github.com/kabanero-io/kabanero-operator/pkg/controller/utils"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveIndex returns a structure representation of the yaml file represented by the index.
func ResolveIndex(c client.Client, repoConf kabanerov1alpha2.RepositoryConfig, namespace string, pipelines []Pipelines, triggers []Trigger, imagePrefix string, reqLogger logr.Logger) (*Index, error) {
	var indexBytes []byte

	switch {
	// GIT:
	case isGitReleaseUsable(repoConf.GitRelease):
		bytes, err := getStackDataUsingGit(c, repoConf.GitRelease, namespace, reqLogger)
		if err != nil {
			return nil, err
		}
		indexBytes = bytes
	// HTTPS:
	case len(repoConf.Https.Url) != 0:
		bytes, err := getStackIndexUsingHttp(c, repoConf, reqLogger)
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

// Retrieves a stack index file content using HTTP.
func getStackIndexUsingHttp(c client.Client, repoConf kabanerov1alpha2.RepositoryConfig, reqLogger logr.Logger) ([]byte, error) {
	url := repoConf.Https.Url

	// user may specify url to yaml file or directory
	matched, err := regexp.MatchString(`/([^/]+)[.]yaml$`, url)
	if err != nil {
		return nil, err
	}
	if !matched {
		url = url + "/index.yaml"
	}

	var oauthToken string
	
	// check to see if we should use oauth.  If so, need to get our service account token.
	if repoConf.Https.OpenShiftOAuth == true {
		secret, err := utils.GetMatchingSecret(c, "kabanero", serviceAccountTokenSecretFilter)
		if err != nil {
			return nil, err
		}

		token, ok := secret.Data["token"]
		if ok == false {
			return nil, fmt.Errorf("Matching secret %v did not contain a token", secret.Name)
		}

		/* 
		fmt.Printf(" !!TDK retrieved token string from secret: %v\n", string(token))
		token, err = base64.StdEncoding.DecodeString(string(token))
		if err != nil {
			return nil, err
		}
    */
		oauthToken = string(token)
		fmt.Printf(" !!TDK retrieved token from the secret: %v\n", oauthToken)
	}
	
	return getFromCache(url, repoConf.Https.SkipCertVerification, oauthToken)
}

// Find the secret with our service account token.
func serviceAccountTokenSecretFilter(secretList *corev1.SecretList, filterStrings ...string) (*corev1.Secret, error) {
	for _, secret := range secretList.Items {
		annotations := secret.GetAnnotations()
		for key, value := range annotations {
			if (key == "kubernetes.io/service-account.name") && (value == "kabanero-operator") {
				if secret.Type == "kubernetes.io/service-account-token" {
					return &secret, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("Unable to find matching secret")
}
