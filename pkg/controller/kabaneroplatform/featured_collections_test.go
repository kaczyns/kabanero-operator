package kabaneroplatform

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	
	kabanerov1alpha1 "github.com/kabanero-io/kabanero-operator/pkg/apis/kabanero/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"testing"
)

// -----------------------------------------------------------------------------------------------
// Client that creates/deletes collections.
// -----------------------------------------------------------------------------------------------
type unitTestClient struct {
	objs map[string]*kabanerov1alpha1.Collection
}

func (c unitTestClient) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	fmt.Printf("Received Get() for %v\n", key.Name)
	u, ok := obj.(*kabanerov1alpha1.Collection)
	if !ok {
		fmt.Printf("Received invalid target object for get: %v\n", obj)
		return errors.New("Get only supports Collections")
	}
	collection := c.objs[key.Name]
	if collection == nil {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}
	collection.DeepCopyInto(u)
	return nil
}
func (c unitTestClient) List(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
	u, ok := list.(*kabanerov1alpha1.CollectionList)
	if !ok {
		fmt.Printf("Received invalid list: %v\n", list)
		return errors.New("List only supports CollectionList")
	}

	fmt.Printf("Received List()\n")
	for _, value := range c.objs {
		u.Items = append(u.Items, *value)
	}
	return nil
}
func (c unitTestClient) Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error {
	u, ok := obj.(*kabanerov1alpha1.Collection)
	if !ok {
		fmt.Printf("Received invalid create: %v\n", obj)
		return errors.New("Create only supports Collections")
	}

	fmt.Printf("Received Create() for %v\n", u.Name)
	collection := c.objs[u.Name]
	if collection != nil {
		fmt.Printf("Receive create object already exists: %v\n", u.Name)
		return apierrors.NewAlreadyExists(schema.GroupResource{}, u.Name)
	}

	c.objs[u.Name] = u
	return nil
}
func (c unitTestClient)	Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOption) error {
	u, ok := obj.(*kabanerov1alpha1.Collection)
	if !ok {
		fmt.Printf("Received invalid delete: %v\n", obj)
		return errors.New("Delete only supports Collections")
	}

	fmt.Printf("Received Delete() for %v\n", u.Name)
	_, ok = c.objs[u.Name]
	if !ok {
		fmt.Printf("Receive delete object does not exist: %v\n", u.Name)
		return apierrors.NewNotFound(schema.GroupResource{}, u.GetName())
	}

	delete(c.objs, u.Name)
	return nil
}
func (c unitTestClient) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	u, ok := obj.(*kabanerov1alpha1.Collection)
	if !ok {
		fmt.Printf("Received invalid update: %v\n", obj)
		return errors.New("Update only supports Collections")
	}

	fmt.Printf("Received Update() for %v\n", u.Name)
	collection := c.objs[u.Name]
	if collection == nil {
		fmt.Printf("Received update for object that does not exist: %v\n", obj)
		return apierrors.NewNotFound(schema.GroupResource{}, u.Name)
	}
	c.objs[u.Name] = u
	return nil
}
func (c unitTestClient) Status() client.StatusWriter { return c }
func (c unitTestClient) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	return errors.New("Patch is not supported")
}
func (c unitTestClient) DeleteAllOf(ctx context.Context, obj runtime.Object, opts ...client.DeleteAllOfOption) error {
	return errors.New("DeleteAllOf is not supported")
}

// -----------------------------------------------------------------------------------------------
// HTTP handler that serves kabanero indexes
// -----------------------------------------------------------------------------------------------
type collectionIndexHandler struct {
}

func (ch collectionIndexHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	filename := fmt.Sprintf("testdata/%v", req.URL.String())
	fmt.Printf("Serving %v\n", filename)
	d, err := ioutil.ReadFile(filename)
	if err != nil {
		rw.WriteHeader(http.StatusNotFound)
	} else {
		rw.Write(d)
	}
}

var defaultIndexName = "/kabanero-index.yaml"
var secondIndexName = "/kabanero-index-two.yaml"

// -----------------------------------------------------------------------------------------------
// Test cases
// -----------------------------------------------------------------------------------------------
func createKabanero(repositoryUrl string, activateDefaultCollections bool) *kabanerov1alpha1.Kabanero {
	return &kabanerov1alpha1.Kabanero{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kabanero.io/v1alpha1",
			Kind:       "Kabanero",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "kabanero",
			UID:       "12345",
		},
		Spec: kabanerov1alpha1.KabaneroSpec{
			Collections: kabanerov1alpha1.InstanceCollectionConfig{
				Repositories: []kabanerov1alpha1.RepositoryConfig{
					kabanerov1alpha1.RepositoryConfig{
						Name:                       "default",
						Url:                        repositoryUrl,
						ActivateDefaultCollections: activateDefaultCollections,
					},
				},
			},
		},
	}
}

func createCollection(k *kabanerov1alpha1.Kabanero, name string, version string, desiredState string) *kabanerov1alpha1.Collection{
	return &kabanerov1alpha1.Collection{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kabanero.io/v1alpha1",
			Kind:       "Collection",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
			UID:       "12345",
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: k.APIVersion,
					Kind:       k.Kind,
					Name:       k.Name,
					UID:        k.UID,
				},
			},
		},
		Spec: kabanerov1alpha1.CollectionSpec{
			RepositoryUrl: "http://example.com",
			Name: name,
			Version: version,
			DesiredState: desiredState,
			Versions: []kabanerov1alpha1.CollectionVersion{
				kabanerov1alpha1.CollectionVersion{
					RepositoryUrl: "http://example.com",
					Version: version,
					DesiredState: desiredState,
				},
			},
		},
	}
}

func TestReconcileFeaturedCollections(t *testing.T) {
	// The server that will host the pipeline zip
	server := httptest.NewServer(collectionIndexHandler{})
	defer server.Close()

	ctx := context.Background()
	cl := unitTestClient{make(map[string]*kabanerov1alpha1.Collection)}
	collectionUrl := server.URL + defaultIndexName
	k := createKabanero(collectionUrl, true)

	err := reconcileFeaturedCollections(ctx, k, cl)
	if err != nil {
		t.Fatal(err)
	}

	// Should have been two collections created
	javaMicroprofileCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "java-microprofile"}, javaMicroprofileCollection)
	if err != nil {
		t.Fatal("Could not resolve the java-microprofile collection", err)
	}

	nodejsCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "nodejs"}, nodejsCollection)
	if err != nil {
		t.Fatal("Could not resolve the nodejs collection", err)
	}

	// Make sure the collection has an owner set
	if len(nodejsCollection.OwnerReferences) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 owner, but found %v: %v", len(nodejsCollection.OwnerReferences), nodejsCollection))
	}

	if nodejsCollection.OwnerReferences[0].UID != k.UID {
		t.Fatal(fmt.Sprintf("Expected owner UID to be %v, but was %v", k.UID, nodejsCollection.OwnerReferences[0].UID))
	}

	// Make sure the collection is active
	if len(nodejsCollection.Spec.Versions) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 collection version, but found %v: %v", len(nodejsCollection.Spec.Versions), nodejsCollection.Spec.Versions))
	}

	if nodejsCollection.Spec.Versions[0].Version != "0.2.6" {
		t.Fatal(fmt.Sprintf("Expected nodejs collection version \"0.2.6\", but found %v", nodejsCollection.Spec.Versions[0].Version))
	}

	if nodejsCollection.Spec.Versions[0].DesiredState != "" {
		t.Fatal(fmt.Sprintf("Expected nodejs collection desiredState to be empty %v", nodejsCollection.Spec.Versions[0].DesiredState))
	}

	if nodejsCollection.Spec.Versions[0].RepositoryUrl != collectionUrl {
		t.Fatal(fmt.Sprintf("Expected nodejs URL to be %v, but was %v", collectionUrl, nodejsCollection.Spec.Versions[0].RepositoryUrl))
	}
}

// Test that we remove a version of the collection that is no longer in the index
func TestReconcileFeaturedCollectionsRemoveOldVersion(t *testing.T) {
	// The server that will host the pipeline zip
	server := httptest.NewServer(collectionIndexHandler{})
	defer server.Close()

	ctx := context.Background()
	cl := unitTestClient{make(map[string]*kabanerov1alpha1.Collection)}
	collectionUrl := server.URL + defaultIndexName
	k := createKabanero(collectionUrl, true)

	existingCollection := createCollection(k, "nodejs", "0.0.1", "")
	err := cl.Create(ctx, existingCollection)
	if err != nil {
		t.Fatal(err)
	}
	
	err = reconcileFeaturedCollections(ctx, k, cl)
	if err != nil {
		t.Fatal(err)
	}

	// Should have been two collections created
	javaMicroprofileCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "java-microprofile"}, javaMicroprofileCollection)
	if err != nil {
		t.Fatal("Could not resolve the java-microprofile collection", err)
	}

	nodejsCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "nodejs"}, nodejsCollection)
	if err != nil {
		t.Fatal("Could not resolve the nodejs collection", err)
	}

	// Make sure the collection has an owner set
	if len(nodejsCollection.OwnerReferences) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 owner, but found %v: %v", len(nodejsCollection.OwnerReferences), nodejsCollection))
	}

	if nodejsCollection.OwnerReferences[0].UID != k.UID {
		t.Fatal(fmt.Sprintf("Expected owner UID to be %v, but was %v", k.UID, nodejsCollection.OwnerReferences[0].UID))
	}

	// Make sure the collection is active
	if len(nodejsCollection.Spec.Versions) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 collection version, but found %v: %v", len(nodejsCollection.Spec.Versions), nodejsCollection.Spec.Versions))
	}

	if nodejsCollection.Spec.Versions[0].Version != "0.2.6" {
		t.Fatal(fmt.Sprintf("Expected nodejs collection version \"0.2.6\", but found %v", nodejsCollection.Spec.Versions[0].Version))
	}

	if nodejsCollection.Spec.Versions[0].DesiredState != "" {
		t.Fatal(fmt.Sprintf("Expected nodejs collection desiredState to be empty %v", nodejsCollection.Spec.Versions[0].DesiredState))
	}

	if nodejsCollection.Spec.Versions[0].RepositoryUrl != collectionUrl {
		t.Fatal(fmt.Sprintf("Expected nodejs URL to be %v, but was %v", collectionUrl, nodejsCollection.Spec.Versions[0].RepositoryUrl))
	}
}

// Test that we remove a collection that is no longer in the index
func TestReconcileFeaturedCollectionsRemoveDeletedCollection(t *testing.T) {
	// The server that will host the pipeline zip
	server := httptest.NewServer(collectionIndexHandler{})
	defer server.Close()

	ctx := context.Background()
	cl := unitTestClient{make(map[string]*kabanerov1alpha1.Collection)}
	collectionUrl := server.URL + defaultIndexName
	k := createKabanero(collectionUrl, true)

	existingCollection := createCollection(k, "cobol", "0.0.1", "")
	err := cl.Create(ctx, existingCollection)
	if err != nil {
		t.Fatal(err)
	}
	
	err = reconcileFeaturedCollections(ctx, k, cl)
	if err != nil {
		t.Fatal(err)
	}

	// Should have been two collections created
	collectionList := &kabanerov1alpha1.CollectionList{}
	err = cl.List(ctx, collectionList)
	if err != nil {
		t.Fatal("Could not list collections", err)
	}

	if len(collectionList.Items) != 2 {
		t.Fatal(fmt.Sprintf("Expected 2 collections, but was %v: %v", len(collectionList.Items), collectionList.Items))
	}
	
	javaMicroprofileCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "java-microprofile"}, javaMicroprofileCollection)
	if err != nil {
		t.Fatal("Could not resolve the java-microprofile collection", err)
	}

	nodejsCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "nodejs"}, nodejsCollection)
	if err != nil {
		t.Fatal("Could not resolve the nodejs collection", err)
	}

	// Make sure the collection has an owner set
	if len(nodejsCollection.OwnerReferences) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 owner, but found %v: %v", len(nodejsCollection.OwnerReferences), nodejsCollection))
	}

	if nodejsCollection.OwnerReferences[0].UID != k.UID {
		t.Fatal(fmt.Sprintf("Expected owner UID to be %v, but was %v", k.UID, nodejsCollection.OwnerReferences[0].UID))
	}

	// Make sure the collection is active
	if len(nodejsCollection.Spec.Versions) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 collection version, but found %v: %v", len(nodejsCollection.Spec.Versions), nodejsCollection.Spec.Versions))
	}

	if nodejsCollection.Spec.Versions[0].Version != "0.2.6" {
		t.Fatal(fmt.Sprintf("Expected nodejs collection version \"0.2.6\", but found %v", nodejsCollection.Spec.Versions[0].Version))
	}

	if nodejsCollection.Spec.Versions[0].DesiredState != "" {
		t.Fatal(fmt.Sprintf("Expected nodejs collection desiredState to be empty %v", nodejsCollection.Spec.Versions[0].DesiredState))
	}

	if nodejsCollection.Spec.Versions[0].RepositoryUrl != collectionUrl {
		t.Fatal(fmt.Sprintf("Expected nodejs URL to be %v, but was %v", collectionUrl, nodejsCollection.Spec.Versions[0].RepositoryUrl))
	}
}

// Test that we retain a collection that is no longer in the index if its desiredState is set.
func TestReconcileFeaturedCollectionsRetainDeletedCollection(t *testing.T) {
	// The server that will host the pipeline zip
	server := httptest.NewServer(collectionIndexHandler{})
	defer server.Close()

	ctx := context.Background()
	cl := unitTestClient{make(map[string]*kabanerov1alpha1.Collection)}
	collectionUrl := server.URL + defaultIndexName
	k := createKabanero(collectionUrl, true)

	existingCollection := createCollection(k, "cobol", "0.0.1", "active")
	err := cl.Create(ctx, existingCollection)
	if err != nil {
		t.Fatal(err)
	}
	
	err = reconcileFeaturedCollections(ctx, k, cl)
	if err != nil {
		t.Fatal(err)
	}

	// Should have been three collections created
	collectionList := &kabanerov1alpha1.CollectionList{}
	err = cl.List(ctx, collectionList)
	if err != nil {
		t.Fatal("Could not list collections", err)
	}

	if len(collectionList.Items) != 3 {
		t.Fatal(fmt.Sprintf("Expected 3 collections, but was %v: %v", len(collectionList.Items), collectionList.Items))
	}
	
	javaMicroprofileCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "java-microprofile"}, javaMicroprofileCollection)
	if err != nil {
		t.Fatal("Could not resolve the java-microprofile collection", err)
	}

	nodejsCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "nodejs"}, nodejsCollection)
	if err != nil {
		t.Fatal("Could not resolve the nodejs collection", err)
	}

	cobolCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "cobol"}, cobolCollection)
	if err != nil {
		t.Fatal("Could not resolve the cobol collection", err)
	}
	
	// Make sure the collection is active
	if len(cobolCollection.Spec.Versions) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 collection version, but found %v: %v", len(cobolCollection.Spec.Versions), cobolCollection.Spec.Versions))
	}

	if cobolCollection.Spec.Versions[0].Version != "0.0.1" {
		t.Fatal(fmt.Sprintf("Expected cobol collection version \"0.0.1\", but found %v", cobolCollection.Spec.Versions[0].Version))
	}

	if cobolCollection.Spec.Versions[0].DesiredState != "active" {
		t.Fatal(fmt.Sprintf("Expected cobol collection desiredState to be active, but was %v", cobolCollection.Spec.Versions[0].DesiredState))
	}
}

// Test that we leave a previously-overridden collection alone.
func TestReconcileFeaturedCollectionsWithExistingOverride(t *testing.T) {
	// The server that will host the pipeline zip
	server := httptest.NewServer(collectionIndexHandler{})
	defer server.Close()

	ctx := context.Background()
	cl := unitTestClient{make(map[string]*kabanerov1alpha1.Collection)}
	collectionUrl := server.URL + defaultIndexName
	k := createKabanero(collectionUrl, true)

	existingCollection := createCollection(k, "java-microprofile", "0.0.1", "inactive")
	err := cl.Create(ctx, existingCollection)
	if err != nil {
		t.Fatal(err)
	}
	
	err = reconcileFeaturedCollections(ctx, k, cl)
	if err != nil {
		t.Fatal(err)
	}

	// Should have been two collections created
	javaMicroprofileCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "java-microprofile"}, javaMicroprofileCollection)
	if err != nil {
		t.Fatal("Could not resolve the java-microprofile collection", err)
	}

	nodejsCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "nodejs"}, nodejsCollection)
	if err != nil {
		t.Fatal("Could not resolve the nodejs collection", err)
	}

	// Make sure the collection has an owner set
	if len(nodejsCollection.OwnerReferences) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 owner, but found %v: %v", len(nodejsCollection.OwnerReferences), nodejsCollection))
	}

	if nodejsCollection.OwnerReferences[0].UID != k.UID {
		t.Fatal(fmt.Sprintf("Expected owner UID to be %v, but was %v", k.UID, nodejsCollection.OwnerReferences[0].UID))
	}

	// Make sure the collection is active
	if len(nodejsCollection.Spec.Versions) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 collection version, but found %v: %v", len(nodejsCollection.Spec.Versions), nodejsCollection.Spec.Versions))
	}

	if nodejsCollection.Spec.Versions[0].Version != "0.2.6" {
		t.Fatal(fmt.Sprintf("Expected nodejs collection version \"0.2.6\", but found %v", nodejsCollection.Spec.Versions[0].Version))
	}

	if nodejsCollection.Spec.Versions[0].DesiredState != "" {
		t.Fatal(fmt.Sprintf("Expected nodejs collection desiredState to be empty %v", nodejsCollection.Spec.Versions[0].DesiredState))
	}

	if nodejsCollection.Spec.Versions[0].RepositoryUrl != collectionUrl {
		t.Fatal(fmt.Sprintf("Expected nodejs URL to be %v, but was %v", collectionUrl, nodejsCollection.Spec.Versions[0].RepositoryUrl))
	}

	if len(javaMicroprofileCollection.Spec.Versions) != 2 {
		t.Fatal(fmt.Sprintf("Expected 2 collection versions, but found %v: %v", len(javaMicroprofileCollection.Spec.Versions), javaMicroprofileCollection.Spec.Versions))
	}

	if javaMicroprofileCollection.Spec.DesiredState != "inactive" {
		t.Fatal(fmt.Sprintf("Expected singleton desiredState to be inactive, but was %v", javaMicroprofileCollection.Spec.DesiredState))
	}

	if javaMicroprofileCollection.Spec.DesiredState != javaMicroprofileCollection.Spec.Versions[0].DesiredState {
		t.Fatal(fmt.Sprintf("Singleton desiredState was not consistent with first version array element: %v", javaMicroprofileCollection.Spec))
	}
}

// Test that we leave a previously-overridden collection alone when the same version is present in the collection hub.
func TestReconcileFeaturedCollectionsWithExistingOverrideSameVersion(t *testing.T) {
	// The server that will host the pipeline zip
	server := httptest.NewServer(collectionIndexHandler{})
	defer server.Close()

	ctx := context.Background()
	cl := unitTestClient{make(map[string]*kabanerov1alpha1.Collection)}
	collectionUrl := server.URL + defaultIndexName
	k := createKabanero(collectionUrl, true)

	existingCollection := createCollection(k, "java-microprofile", "0.2.19", "inactive")
	err := cl.Create(ctx, existingCollection)
	if err != nil {
		t.Fatal(err)
	}
	
	err = reconcileFeaturedCollections(ctx, k, cl)
	if err != nil {
		t.Fatal(err)
	}

	// Should have been two collections created.  We're not going to go crazy verifying the nodejs collection since other
	// tests do that.
	javaMicroprofileCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "java-microprofile"}, javaMicroprofileCollection)
	if err != nil {
		t.Fatal("Could not resolve the java-microprofile collection", err)
	}

	nodejsCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "nodejs"}, nodejsCollection)
	if err != nil {
		t.Fatal("Could not resolve the nodejs collection", err)
	}

	if len(javaMicroprofileCollection.Spec.Versions) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 collection version, but found %v: %v", len(javaMicroprofileCollection.Spec.Versions), javaMicroprofileCollection.Spec.Versions))
	}

	if javaMicroprofileCollection.Spec.DesiredState != "inactive" {
		t.Fatal(fmt.Sprintf("Expected singleton desiredState to be inactive, but was %v", javaMicroprofileCollection.Spec.DesiredState))
	}

	if javaMicroprofileCollection.Spec.DesiredState != javaMicroprofileCollection.Spec.Versions[0].DesiredState {
		t.Fatal(fmt.Sprintf("Singleton desiredState was not consistent with first version array element: %v", javaMicroprofileCollection.Spec))
	}

	if javaMicroprofileCollection.Spec.RepositoryUrl != existingCollection.Spec.RepositoryUrl {
		t.Fatal(fmt.Sprintf("Expected singleton repository URL to be %v, but was %v", existingCollection.Spec.RepositoryUrl, javaMicroprofileCollection.Spec.RepositoryUrl))
	}

	if javaMicroprofileCollection.Spec.RepositoryUrl != javaMicroprofileCollection.Spec.Versions[0].RepositoryUrl {
		t.Fatal(fmt.Sprintf("Singleton repositoryUrl was not consistent with the first version array element: %v", javaMicroprofileCollection.Spec))
	}
}

func TestReconcileFeaturedCollectionsTwoRepositories(t *testing.T) {
	// The server that will host the pipeline zip
	server := httptest.NewServer(collectionIndexHandler{})
	defer server.Close()

	ctx := context.Background()
	cl := unitTestClient{make(map[string]*kabanerov1alpha1.Collection)}
	collectionUrl := server.URL + defaultIndexName
	collectionUrlTwo := server.URL + secondIndexName
	k := createKabanero(collectionUrl, true)
	k.Spec.Collections.Repositories = append(k.Spec.Collections.Repositories, kabanerov1alpha1.RepositoryConfig{Name: "two", Url: collectionUrlTwo})

	err := reconcileFeaturedCollections(ctx, k, cl)
	if err != nil {
		t.Fatal(err)
	}

	// Should have been two collections created
	javaMicroprofileCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "java-microprofile"}, javaMicroprofileCollection)
	if err != nil {
		t.Fatal("Could not resolve the java-microprofile collection", err)
	}

	nodejsCollection := &kabanerov1alpha1.Collection{}
	err = cl.Get(ctx, types.NamespacedName{Name: "nodejs"}, nodejsCollection)
	if err != nil {
		t.Fatal("Could not resolve the nodejs collection", err)
	}

	// Make sure the collection has an owner set
	if len(nodejsCollection.OwnerReferences) != 1 {
		t.Fatal(fmt.Sprintf("Expected 1 owner, but found %v: %v", len(nodejsCollection.OwnerReferences), nodejsCollection))
	}

	if nodejsCollection.OwnerReferences[0].UID != k.UID {
		t.Fatal(fmt.Sprintf("Expected owner UID to be %v, but was %v", k.UID, nodejsCollection.OwnerReferences[0].UID))
	}

	// Make sure the collection is in the correct state
	if len(nodejsCollection.Spec.Versions) != 2 {
		t.Fatal(fmt.Sprintf("Expected 2 collection versions, but found %v: %v", len(nodejsCollection.Spec.Versions), nodejsCollection.Spec.Versions))
	}

	foundVersions := make(map[string]bool)
	for _, cur := range nodejsCollection.Spec.Versions {
		foundVersions[cur.Version] = true
		if cur.Version == "0.2.6" {
			if cur.DesiredState != "" {
				t.Fatal(fmt.Sprintf("Expected desiredState for version \"0.2.6\" to be empty, but was %v", cur.DesiredState))
			}
			if cur.RepositoryUrl != collectionUrl {
				t.Fatal(fmt.Sprintf("Expected version \"0.2.6\" URL to be %v, but was %v", collectionUrl, cur.RepositoryUrl))
			}
		} else if cur.Version == "0.4.1" {
			if cur.DesiredState != "" {
				t.Fatal(fmt.Sprintf("Expected desiredState for version \"0.4.1\" to be empty, but was %v", cur.DesiredState))
			}
			if cur.RepositoryUrl != collectionUrlTwo {
				t.Fatal(fmt.Sprintf("Expected version \"0.4.1\" URL to be %v, but was %v", collectionUrlTwo, cur.RepositoryUrl))
			}
		} else {
			t.Fatal(fmt.Sprintf("Found unexpected version %v", cur.Version))
		}
	}

	if foundVersions["0.2.6"] != true {
		t.Fatal("Did not find collection version \"0.2.6\"")
	}

	if foundVersions["0.4.1"] != true {
		t.Fatal("Did not find collection version \"0.4.1\"")
	}
}

// Attempts to resolve the featured collections from the default repository
func TestResolveFeaturedCollections(t *testing.T) {
	// The server that will host the pipeline zip
	server := httptest.NewServer(collectionIndexHandler{})
	defer server.Close()

	collection_index_url := server.URL + defaultIndexName
	k := createKabanero(collection_index_url, true)

	collections, err := featuredCollections(k)
	if err != nil {
		t.Fatal("Could not resolve the featured collections from the default index", err)
	}

	// Should be two collections
	if len(collections) != 2 {
		t.Fatal(fmt.Sprintf("Was expecting 2 collections to be found, but found %v: %v", len(collections), collections))
	}

	javaMicroprofileCollectionVersions, ok := collections["java-microprofile"]
	if !ok {
		t.Fatal(fmt.Sprintf("Could not find java-microprofile collection: %v", collections))
	}

	nodejsCollectionVersions, ok := collections["nodejs"]
	if !ok {
		t.Fatal(fmt.Sprintf("Could not find nodejs collection: %v", collections))
	}

	// Make sure each collection has one version
	if len(javaMicroprofileCollectionVersions) != 1 {
		t.Fatal(fmt.Sprintf("Expected one version of java-microprofile collection, but found %v: %v", len(javaMicroprofileCollectionVersions), javaMicroprofileCollectionVersions))
	}

	if len(nodejsCollectionVersions) != 1 {
		t.Fatal(fmt.Sprintf("Expected one version of nodejs collection, but found %v: %v", len(nodejsCollectionVersions), nodejsCollectionVersions))
	}
}

// Attempts to resolve the featured collections from two repositories
func TestResolveFeaturedCollectionsTwoRepositories(t *testing.T) {
	// The server that will host the pipeline zip
	server := httptest.NewServer(collectionIndexHandler{})
	defer server.Close()

	collection_index_url := server.URL + defaultIndexName
	collection_index_url_two := server.URL + secondIndexName
	k := createKabanero(collection_index_url, true)
	k.Spec.Collections.Repositories = append(k.Spec.Collections.Repositories, kabanerov1alpha1.RepositoryConfig{Name: "two", Url: collection_index_url_two})

	collections, err := featuredCollections(k)
	if err != nil {
		t.Fatal("Could not resolve the featured collections from the default index", err)
	}

	// Should be two collections
	if len(collections) != 2 {
		t.Fatal(fmt.Sprintf("Was expecting 2 collections to be found, but found %v: %v", len(collections), collections))
	}

	javaMicroprofileCollectionVersions, ok := collections["java-microprofile"]
	if !ok {
		t.Fatal(fmt.Sprintf("Could not find java-microprofile collection: %v", collections))
	}

	nodejsCollectionVersions, ok := collections["nodejs"]
	if !ok {
		t.Fatal(fmt.Sprintf("Could not find nodejs collection: %v", collections))
	}

	// Make sure each collection has two versions
	if len(javaMicroprofileCollectionVersions) != 2 {
		t.Fatal(fmt.Sprintf("Expected two versions of java-microprofile collection, but found %v: %v", len(javaMicroprofileCollectionVersions), javaMicroprofileCollectionVersions))
	}

	if len(nodejsCollectionVersions) != 2 {
		t.Fatal(fmt.Sprintf("Expected two versions of nodejs collection, but found %v: %v", len(nodejsCollectionVersions), nodejsCollectionVersions))
	}
}
