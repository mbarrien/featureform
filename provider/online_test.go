//go:build online
// +build online

// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
package provider

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"

	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/featureform/helpers"

	"github.com/alicebob/miniredis"
	pc "github.com/featureform/provider/provider_config"
	pt "github.com/featureform/provider/provider_type"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

func mockRedis() *miniredis.Miniredis {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	return s
}

type OnlineResource struct {
	Entity string
	Value  interface{}
	Type   ValueType
}

type testMember struct {
	t               pt.Type
	subType         string
	c               pc.SerializedConfig
	integrationTest bool
}

var provider = flag.String("provider", "all", "provider to perform test on")

func TestOnlineStores(t *testing.T) {
	err := godotenv.Load("../.env")
	if err != nil {
		fmt.Println(err)
	}

	testFns := map[string]func(*testing.T, OnlineStore){
		"CreateGetTable":     testCreateGetTable,
		"TableAlreadyExists": testTableAlreadyExists,
		"TableNotFound":      testTableNotFound,
		"SetGetEntity":       testSetGetEntity,
		"EntityNotFound":     testEntityNotFound,
		"MassTableWrite":     testMassTableWrite,
		"TypeCasting":        testTypeCasting,
	}

	// Redis (Mock)
	redisMockInit := func(mRedis *miniredis.Miniredis) pc.RedisConfig {
		mockRedisAddr := mRedis.Addr()
		redisMockConfig := &pc.RedisConfig{
			Addr: mockRedisAddr,
		}
		return *redisMockConfig
	}

	//Redis (Live)
	redisInsecureInit := func() pc.RedisConfig {
		redisInsecurePort := os.Getenv("REDIS_INSECURE_PORT")
		insecureAddr := fmt.Sprintf("%s:%s", "localhost", redisInsecurePort)
		redisInsecureConfig := &pc.RedisConfig{
			Addr: insecureAddr,
		}
		return *redisInsecureConfig
	}

	redisSecureInit := func() pc.RedisConfig {
		redisSecurePort := os.Getenv("REDIS_SECURE_PORT")
		redisPassword := os.Getenv("REDIS_PASSWORD")
		secureAddr := fmt.Sprintf("%s:%s", "localhost", redisSecurePort)
		redisSecureConfig := &pc.RedisConfig{
			Addr:     secureAddr,
			Password: redisPassword,
		}
		return *redisSecureConfig
	}

	//Cassandra
	cassandraInit := func() pc.CassandraConfig {
		cassandraAddr := "localhost:9042"
		cassandraUsername := os.Getenv("CASSANDRA_USER")
		cassandraPassword := os.Getenv("CASSANDRA_PASSWORD")
		cassandraConfig := &pc.CassandraConfig{
			Addr:        cassandraAddr,
			Username:    cassandraUsername,
			Consistency: "ONE",
			Password:    cassandraPassword,
			Replication: 3,
		}
		return *cassandraConfig
	}

	//Firestore
	firestoreInit := func() pc.FirestoreConfig {
		projectID := os.Getenv("FIRESTORE_PROJECT")
		firestoreCredentials := os.Getenv("FIRESTORE_CRED")
		JSONCredentials, err := ioutil.ReadFile(firestoreCredentials)
		if err != nil {
			panic(fmt.Sprintf("Could not open firestore credentials: %v", err))
		}

		var credentialsDict map[string]interface{}
		err = json.Unmarshal(JSONCredentials, &credentialsDict)
		if err != nil {
			panic(fmt.Errorf("cannot unmarshal big query credentials: %v", err))
		}

		firestoreConfig := &pc.FirestoreConfig{
			Collection:  "featureform_test",
			ProjectID:   projectID,
			Credentials: credentialsDict,
		}
		return *firestoreConfig
	}

	dynamoInit := func() pc.DynamodbConfig {
		dynamoAccessKey := os.Getenv("DYNAMO_ACCESS_KEY")
		dynamoSecretKey := os.Getenv("DYNAMO_SECRET_KEY")
		dynamoConfig := &pc.DynamodbConfig{
			Region:    "us-east-1",
			AccessKey: dynamoAccessKey,
			SecretKey: dynamoSecretKey,
		}
		return *dynamoConfig
	}

	blobAzureInit := func() pc.OnlineBlobConfig {
		azureConfig := pc.AzureFileStoreConfig{
			AccountName:   helpers.GetEnv("AZURE_ACCOUNT_NAME", ""),
			AccountKey:    helpers.GetEnv("AZURE_ACCOUNT_KEY", ""),
			ContainerName: helpers.GetEnv("AZURE_CONTAINER_NAME", "newcontainer"),
			Path:          "featureform/onlinetesting",
		}
		blobConfig := &pc.OnlineBlobConfig{
			Type:   pc.Azure,
			Config: azureConfig,
		}
		return *blobConfig
	}

	mongoDBInit := func() pc.MongoDBConfig {
		mongoConfig := &pc.MongoDBConfig{
			Host:       helpers.GetEnv("MONGODB_HOST", ""),
			Port:       helpers.GetEnv("MONGODB_PORT", ""),
			Username:   helpers.GetEnv("MONGODB_USERNAME", ""),
			Password:   helpers.GetEnv("MONGODB_PASSWORD", ""),
			Database:   helpers.GetEnv("MONGODB_DATABASE", ""),
			Throughput: 1000,
		}
		return *mongoConfig
	}

	testList := []testMember{}

	if *provider == "memory" || *provider == "" {
		testList = append(testList, testMember{pt.LocalOnline, "", []byte{}, false})
	}
	if *provider == "redis_mock" || *provider == "" {
		miniRedis := mockRedis()
		defer miniRedis.Close()
		testList = append(testList, testMember{pt.RedisOnline, "_MOCK", redisMockInit(miniRedis).Serialized(), false})
	}
	if *provider == "redis_insecure" || *provider == "" {
		testList = append(testList, testMember{pt.RedisOnline, "_INSECURE", redisInsecureInit().Serialized(), true})
	}
	if *provider == "redis_secure" || *provider == "" {
		testList = append(testList, testMember{pt.RedisOnline, "_SECURE", redisSecureInit().Serialized(), true})
	}
	if *provider == "cassandra" || *provider == "" {
		testList = append(testList, testMember{pt.CassandraOnline, "", cassandraInit().Serialized(), true})
	}
	if *provider == "firestore" || *provider == "" {
		testList = append(testList, testMember{pt.FirestoreOnline, "", firestoreInit().Serialize(), true})
	}
	if *provider == "dynamo" || *provider == "" {
		testList = append(testList, testMember{pt.DynamoDBOnline, "", dynamoInit().Serialized(), true})
	}
	if *provider == "azure_blob" || *provider == "" {
		testList = append(testList, testMember{pt.BlobOnline, "_AZURE", blobAzureInit().Serialized(), true})
	}
	if *provider == "mongodb" || *provider == "" {
		testList = append(testList, testMember{pt.MongoDBOnline, "", mongoDBInit().Serialized(), true})
	}

	for _, testItem := range testList {
		if testing.Short() && testItem.integrationTest {
			t.Logf("Skipping %s, because it is an integration test", testItem.t)
			continue
		}
		for name, fn := range testFns {
			provider, err := Get(testItem.t, testItem.c)
			if err != nil {
				t.Fatalf("Failed to get provider %s: %s", testItem.t, err)
			}
			store, err := provider.AsOnlineStore()
			if err != nil {
				t.Fatalf("Failed to use provider %s as OnlineStore: %s", testItem.t, err)
			}
			var prefix string
			if testItem.integrationTest {
				prefix = "INTEGRATION"
			} else {
				prefix = "UNIT"
			}
			testName := fmt.Sprintf("%s%s_%s_%s", testItem.t, testItem.subType, prefix, name)
			t.Run(testName, func(t *testing.T) {
				fn(t, store)
			})
			if err := store.Close(); err != nil {
				t.Fatalf("Failed to close online store %s: %v", testItem.t, err)
			}
		}
	}
}

func randomFeatureVariant() (string, string) {
	return uuid.NewString(), uuid.NewString()
}

func testCreateGetTable(t *testing.T, store OnlineStore) {
	mockFeature, mockVariant := randomFeatureVariant()
	defer store.DeleteTable(mockFeature, mockVariant)
	if tab, err := store.CreateTable(mockFeature, mockVariant, String); tab == nil || err != nil {
		t.Fatalf("Failed to create table: %s", err)
	}
	if tab, err := store.GetTable(mockFeature, mockVariant); tab == nil || err != nil {
		t.Fatalf("Failed to get table: %s", err)
	}
}

func testTableAlreadyExists(t *testing.T, store OnlineStore) {
	mockFeature, mockVariant := randomFeatureVariant()
	defer store.DeleteTable(mockFeature, mockVariant)
	if _, err := store.CreateTable(mockFeature, mockVariant, String); err != nil {
		t.Fatalf("Failed to create table: %s", err)
	}
	if _, err := store.CreateTable(mockFeature, mockVariant, String); err == nil {
		t.Fatalf("Succeeded in creating table twice")
	} else if casted, valid := err.(*TableAlreadyExists); !valid {
		t.Fatalf("Wrong error for table already exists: %T", err)
	} else if casted.Error() == "" {
		t.Fatalf("TableAlreadyExists has empty error message")
	}
}

func testTableNotFound(t *testing.T, store OnlineStore) {
	mockFeature, mockVariant := randomFeatureVariant()
	if _, err := store.GetTable(mockFeature, mockVariant); err == nil {
		t.Fatalf("Succeeded in getting non-existent table")
	} else if casted, valid := err.(*TableNotFound); !valid {
		t.Fatalf("Wrong error for table not found: %s,%T", err, err)
	} else if casted.Error() == "" {
		t.Fatalf("TableNotFound has empty error message")
	}
}

func testSetGetEntity(t *testing.T, store OnlineStore) {
	mockFeature, mockVariant := randomFeatureVariant()
	defer store.DeleteTable(mockFeature, mockVariant)
	entity, val := "e", "val"
	defer store.DeleteTable(mockFeature, mockVariant)
	tab, err := store.CreateTable(mockFeature, mockVariant, String)
	if err != nil {
		t.Fatalf("Failed to create table: %s", err)
	}
	if err := tab.Set(entity, val); err != nil {
		t.Fatalf("Failed to set entity: %s", err)
	}
	gotVal, err := tab.Get(entity)
	if err != nil {
		t.Fatalf("Failed to get entity: %s", err)
	}
	if !reflect.DeepEqual(val, gotVal) {
		t.Fatalf("Values are not the same %v %v", val, gotVal)
	}
}

func testEntityNotFound(t *testing.T, store OnlineStore) {
	mockFeature, mockVariant := uuid.NewString(), "v"
	entity := "e"
	defer store.DeleteTable(mockFeature, mockVariant)
	tab, err := store.CreateTable(mockFeature, mockVariant, String)
	if err != nil {
		t.Fatalf("Failed to create table: %s", err)
	}
	if _, err := tab.Get(entity); err == nil {
		t.Fatalf("succeeded in getting non-existent entity")
	} else if casted, valid := err.(*EntityNotFound); !valid {
		t.Fatalf("Wrong error for entity not found: %T", err)
	} else if casted.Error() == "" {
		t.Fatalf("EntityNotFound has empty error message")
	}
}

func testMassTableWrite(t *testing.T, store OnlineStore) {
	tableList := make([]ResourceID, 10)
	for i := range tableList {
		mockFeature, mockVariant := randomFeatureVariant()
		tableList[i] = ResourceID{mockFeature, mockVariant, Feature}
	}
	entityList := make([]string, 10)
	for i := range entityList {
		entityList[i] = uuid.New().String()
	}
	for i := range tableList {
		tab, err := store.CreateTable(tableList[i].Name, tableList[i].Variant, ScalarType("int"))
		if err != nil {
			t.Fatalf("could not create table %v in online store: %v", tableList[i], err)
		}
		defer store.DeleteTable(tableList[i].Name, tableList[i].Variant)
		for j := range entityList {
			if err := tab.Set(entityList[j], 1); err != nil {
				t.Fatalf("could not set entity %v in table %v: %v", entityList[j], tableList[i], err)
			}
		}
	}
	for i := range tableList {
		tab, err := store.GetTable(tableList[i].Name, tableList[i].Variant)
		if err != nil {
			t.Fatalf("could not get table %v in online store: %v", tableList[i], err)
		}
		for j := range entityList {
			val, err := tab.Get(entityList[j])
			if err != nil {
				t.Fatalf("could not get entity %v in table %v: %v", entityList[j], tableList[i], err)
			}
			if val != 1 {
				t.Fatalf("could not get correct value from entity list. Wanted %v, got %v", 1, val)
			}
		}
	}
}

func testTypeCasting(t *testing.T, store OnlineStore) {
	onlineResources := []OnlineResource{
		{
			Entity: "a",
			Value:  int(1),
			Type:   Int,
		},
		{
			Entity: "b",
			Value:  int64(1),
			Type:   Int64,
		},
		{
			Entity: "c",
			Value:  float32(1.0),
			Type:   Float32,
		},
		{
			Entity: "d",
			Value:  float64(1.0),
			Type:   Float64,
		},
		{
			Entity: "e",
			Value:  "1.0",
			Type:   String,
		},
		{
			Entity: "f",
			Value:  false,
			Type:   Bool,
		},
	}
	for _, resource := range onlineResources {
		featureName := uuid.New().String()
		tab, err := store.CreateTable(featureName, "", resource.Type)
		if err != nil {
			t.Fatalf("Failed to create table: %s", err)
		}
		if err := tab.Set(resource.Entity, resource.Value); err != nil {
			t.Fatalf("Failed to set entity: %s", err)
		}
		gotVal, err := tab.Get(resource.Entity)
		if err != nil {
			t.Fatalf("Failed to get entity: %s", err)
		}
		if !reflect.DeepEqual(resource.Value, gotVal) {
			t.Fatalf("Values are not the same %v, type %T. %v, type %T", resource.Value, resource.Value, gotVal, gotVal)
		}
		store.DeleteTable(featureName, "")
	}
}

func TestFirestoreConfig_Deserialize(t *testing.T) {
	content, err := ioutil.ReadFile("connection/connection_configs.json")
	if err != nil {
		t.Fatalf(err.Error())
	}
	var payload map[string]interface{}
	err = json.Unmarshal(content, &payload)
	if err != nil {
		t.Fatalf(err.Error())
	}
	testConfig := payload["Firestore"].(map[string]interface{})

	fsconfig := pc.FirestoreConfig{
		ProjectID:   testConfig["ProjectID"].(string),
		Collection:  testConfig["Collection"].(string),
		Credentials: testConfig["Credentials"].(map[string]interface{}),
	}

	serialized := fsconfig.Serialize()

	type fields struct {
		Collection  string
		ProjectID   string
		Credentials map[string]interface{}
	}
	type args struct {
		config pc.SerializedConfig
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "TestCredentials",
			fields: fields{
				ProjectID:   testConfig["ProjectID"].(string),
				Collection:  testConfig["Collection"].(string),
				Credentials: testConfig["Credentials"].(map[string]interface{}),
			},
			args: args{
				config: serialized,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &pc.FirestoreConfig{
				Collection:  tt.fields.Collection,
				ProjectID:   tt.fields.ProjectID,
				Credentials: tt.fields.Credentials,
			}
			if err := r.Deserialize(tt.args.config); (err != nil) != tt.wantErr {
				t.Errorf("Deserialize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOnlineVectorStores(t *testing.T) {
	err := godotenv.Load("../.env")
	if err != nil {
		fmt.Println(err)
	}

	testFns := map[string]func(*testing.T, OnlineStore){
		"VectorStoreTypeAssertion": testVectorStoreTypeAssertion,
		"CreateIndex":              testCreateIndex,
		"GetSet":                   testGetSet,
		"Nearest":                  testNearest,
	}

	// RediSearch (hosted)
	redisInsecureInit := func() pc.RedisConfig {
		redisearchPort := os.Getenv("REDISEARCH_INSECURE_PORT")
		insecureAddr := fmt.Sprintf("%s:%s", "localhost", redisearchPort)
		redisInsecureConfig := &pc.RedisConfig{
			Addr: insecureAddr,
		}
		return *redisInsecureConfig
	}

	testList := []testMember{}

	if *provider == "redis_vector" || *provider == "" {
		testList = append(testList, testMember{pt.RedisOnline, "_VECTOR", redisInsecureInit().Serialized(), true})
	}

	for _, testItem := range testList {
		if testing.Short() && testItem.integrationTest {
			t.Logf("Skipping %s, because it is an integration test", testItem.t)
			continue
		}
		for name, fn := range testFns {
			provider, err := Get(testItem.t, testItem.c)
			if err != nil {
				t.Fatalf("Failed to get provider %s: %s", testItem.t, err)
			}
			store, err := provider.AsOnlineStore()
			if err != nil {
				t.Fatalf("Failed to use provider %s as OnlineStore: %s", testItem.t, err)
			}
			var prefix string
			if testItem.integrationTest {
				prefix = "INTEGRATION"
			} else {
				prefix = "UNIT"
			}
			testName := fmt.Sprintf("%s%s_%s_%s", testItem.t, testItem.subType, prefix, name)
			t.Run(testName, func(t *testing.T) {
				fn(t, store)
			})
			if err := store.Close(); err != nil {
				t.Fatalf("Failed to close online store %s: %v", testItem.t, err)
			}
		}
	}
}

func testVectorStoreTypeAssertion(t *testing.T, store OnlineStore) {
	_, isVectorStore := store.(VectorStore)
	if !isVectorStore {
		t.Fatalf("Expected VectorStore but received %T", store)
	}
}

func testCreateIndex(t *testing.T, store OnlineStore) {
	mockFeature, mockVariant := randomFeatureVariant()
	vectorStore, isVectorStore := store.(VectorStore)
	if !isVectorStore {
		t.Fatalf("Expected VectorStore but received %T", store)
	}
	vectorType := VectorType{
		ScalarType:  Float32,
		Dimension:   768,
		IsEmbedding: true,
	}
	if vectorTable, err := vectorStore.CreateIndex(mockFeature, mockVariant, vectorType); vectorTable == nil || err != nil {
		t.Fatalf("Failed to create index: %s", err)
	}
}

func testGetSet(t *testing.T, store OnlineStore) {
	mockFeature, mockVariant := randomFeatureVariant()
	vectorStore, isVectorStore := store.(VectorStore)
	if !isVectorStore {
		t.Errorf("Expected VectorStore but received %T", store)
	}
	vectorType := VectorType{
		ScalarType:  Float32,
		Dimension:   768,
		IsEmbedding: true,
	}
	vTbl, err := vectorStore.CreateIndex(mockFeature, mockVariant, vectorType)
	if vTbl == nil || err != nil {
		t.Fatalf("Failed to create index: %s", err)
	}
	onTbl, err := store.CreateTable(mockFeature, mockVariant, vectorType)
	if onTbl == nil || err != nil {
		t.Fatalf("Failed to create index: %s", err)
	}
	tbl, err := store.GetTable(mockFeature, mockVariant)
	if tbl == nil || err != nil {
		t.Fatalf("Failed to create index: %s", err)
	}
	vectorTable, ok := tbl.(VectorStoreTable)
	if !ok {
		t.Fatalf("Expected table to be VectorStoreTable but received:  %T", tbl)
	}
	entities := getTestVectorEntities(t)
	for _, entity := range entities {
		if err := vectorTable.Set(entity.entity, entity.vector); err != nil {
			t.Fatalf("Failed to set vector: %s", err)
		}
		if vector, err := vectorTable.Get(entity.entity); err != nil {
			t.Fatalf("Failed to get vector: %s", err)
		} else {
			if !reflect.DeepEqual(vector, entity.vector) {
				t.Fatalf("Expected vector %v but received %v", entity.vector, vector)
			}
		}
	}
}

func testNearest(t *testing.T, store OnlineStore) {
	mockFeature, mockVariant := randomFeatureVariant()
	vectorStore, isVectorStore := store.(VectorStore)
	if !isVectorStore {
		t.Fatalf("Expected VectorStore but received %T", store)
	}
	vectorType := VectorType{
		ScalarType:  Float32,
		Dimension:   768,
		IsEmbedding: true,
	}
	vTbl, err := vectorStore.CreateIndex(mockFeature, mockVariant, vectorType)
	if vTbl == nil || err != nil {
		t.Fatalf("Failed to create index: %s", err)
	}
	onTbl, err := store.CreateTable(mockFeature, mockVariant, vectorType)
	if onTbl == nil || err != nil {
		t.Fatalf("Failed to create index: %s", err)
	}
	tbl, err := store.GetTable(mockFeature, mockVariant)
	if tbl == nil || err != nil {
		t.Fatalf("Failed to create index: %s", err)
	}
	vectorTable, ok := tbl.(VectorStoreTable)
	if !ok {
		t.Fatalf("Expected table to be VectorStoreTable but received:  %T", tbl)
	}
	entities := getTestVectorEntities(t)
	for _, entity := range entities {
		if err := vectorTable.Set(entity.entity, entity.vector); err != nil {
			t.Fatalf("Failed to set vector: %s", err)
		}
	}
	searchVector := getSearchVector(t)
	results, err := vectorTable.Nearest(mockFeature, mockVariant, searchVector, 2)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Expected 2 results but received %d", len(results))
	}
}

type testEmbeddingRecord struct {
	entity string
	vector []float32
}

func getTestVectorEntities(t *testing.T) []testEmbeddingRecord {
	file, err := os.Open("test_files/embeddings.csv")
	if err != nil {
		t.Errorf("Failed to open embedding.csv: %s", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read and discard the header
	_, err = reader.Read()
	if err != nil {
		t.Errorf("Failed to read header: %s", err)
	}

	records := []testEmbeddingRecord{}
	rows, err := reader.ReadAll()
	if err != nil {
		t.Errorf("Failed to read all rows: %s", err)
	}
	for _, row := range rows {
		strFloats := strings.Split(row[1], ",")
		floats := make([]float32, len(strFloats))
		for i, str := range strFloats {
			f, err := strconv.ParseFloat(str, 32)
			if err != nil {
				t.Errorf("Failed to parse float: %s", err)
			}
			floats[i] = float32(f)
		}

		record := testEmbeddingRecord{
			entity: row[0],
			vector: floats,
		}

		records = append(records, record)
	}

	return records
}

func getSearchVector(t *testing.T) []float32 {
	vectorStr := "-0.0076010902,-0.035518534,-0.0049797683,0.050222702,-0.019344976,-0.040610023,0.017240098,-0.02837655,-0.04555259,-0.08093673,-0.07633236,-0.019338677,0.01213481,-0.07509798,-0.06270343,0.009127783,0.04006814,0.0010386414,0.0004191131,0.037658963,0.012474329,0.032541756,-0.008778897,0.022611657,-0.04410798,0.00041922208,-0.011818334,-0.0026080708,-0.048903722,0.0089447405,0.046308264,0.04129424,0.032506913,0.00701404,-0.020091388,0.0045237443,0.06267319,0.026267748,-0.02074065,-0.040799033,0.08098149,0.054281574,-0.006898406,-0.03876563,-0.006502788,0.042736202,0.0014893615,0.07090165,0.0068973983,-0.0047194352,-0.03165896,-0.020197777,-0.039371926,-0.000310349,0.020461414,0.0001061841,0.011596735,0.09024296,-0.008337601,-0.031783745,0.0934002,0.0030525713,0.03262889,0.05029709,-0.028481092,-0.023019888,-0.012560676,-0.032377794,0.032080207,0.021492975,0.028832199,0.036723763,0.00738864,0.05466118,0.056315698,-0.012901263,0.0075963372,-0.045920525,0.048377376,0.012237975,0.025780542,0.080603905,-0.009543171,0.027632339,-0.021400549,-0.09328193,-0.03064339,0.0065343594,-0.034189016,-0.024710312,0.016833825,-0.022612996,-0.031191293,0.0016718105,0.024289448,-0.039289813,-0.026647199,0.052636437,0.029904308,-0.06718917,-0.022759072,-0.016310416,0.028863566,-0.015082388,-0.001870304,0.0040566972,0.008472028,-0.035554465,-0.0022277262,-0.049323037,-0.0206662,-0.05510659,0.019690055,0.04393251,0.021763781,0.025636502,0.021698471,0.019996032,-0.043058433,-0.007352008,0.010921852,-0.03127264,-0.0244568,0.031799316,0.0401328,0.017222589,-0.031441133,-0.012933128,-0.019515522,-0.00025695813,-0.021266801,0.02082639,0.0070872954,-0.059487604,-0.025033496,-0.016842537,-0.058546174,0.011597212,-0.045752943,-0.008808277,0.06766411,-0.0049724537,-0.023061216,0.019976346,0.062111843,0.054523874,0.013280705,0.0075405296,-0.033306547,-0.01993328,0.05988995,0.018138269,-0.0064286655,-0.07123405,0.029928915,-0.05795571,0.07510921,0.03356708,0.004450344,-0.06327657,0.022359971,0.050405357,0.021742715,0.04826851,-0.034396745,-0.0020569703,0.0343405,-0.0051202504,-0.01128138,-0.07150764,0.011326808,0.06636758,-0.011546056,0.007188156,-0.012215938,0.034156535,0.022746291,0.014285776,-0.050845034,0.02438735,-0.004307706,-0.025970276,0.031123316,-0.04344904,0.026749069,-0.045574695,-0.009198401,0.028616162,-0.028526768,0.007191774,-0.0062985998,0.0037683481,-0.017853124,0.01673152,-0.038109444,0.02663099,-0.0035730898,0.018429143,0.054710347,-0.042427346,-0.026992667,0.002804039,-0.0018993038,-0.044040587,0.0023106425,0.00276229,-0.07525604,0.027604144,-0.021161,0.049840633,-0.055751782,0.06643764,0.048756685,-0.06803919,-0.036918934,-0.028683238,-0.049581036,0.051108968,0.005002289,-0.05659039,0.022223396,0.019786311,0.0047440007,-0.018176496,-0.03964165,0.011249621,0.05190393,0.031710673,0.0058737583,0.02813653,-0.008684869,0.041446295,-0.06592884,-0.02078017,0.09003706,0.014742186,0.04654332,0.030603446,0.030613944,-0.021355467,-0.014549738,-0.003864002,0.013592668,0.016913751,0.04112241,0.018864745,0.03859178,0.0343625,-0.045205228,-0.003972531,-0.020700097,-0.025793856,-0.018192774,-0.018161109,-0.001183942,0.075215556,-0.04196815,0.06779689,-0.00825021,0.054049537,0.010641467,0.0491232,0.06801408,0.025885712,0.021657724,0.043146998,-0.00644975,0.014924411,-0.014815088,0.050768893,0.06380339,-0.006740734,0.02806364,0.00024864145,0.0057423306,-0.0024444314,0.04702468,0.011492536,0.0013276701,-0.037540942,-0.03314813,-0.003613196,0.02494995,-0.0065969173,0.004169133,0.04573514,0.022305949,0.0019278156,-0.025791476,-0.004697951,0.0037863906,-0.018920751,-0.031229494,-0.0060712215,-0.0088267485,0.047195204,0.025365528,-0.0012127166,-0.04105116,0.013961203,0.07266988,-0.0030155943,-0.020079784,-0.026650105,-0.0003398677,-0.07031948,0.02555669,-0.06681405,-0.017590746,-0.04496949,0.014884941,-0.03350023,0.037188873,0.037137955,-0.028987778,-0.018978942,-0.034073003,0.041938834,-0.038373213,-0.028766332,0.024883289,0.0612907,0.005968037,0.054267835,-0.035842657,0.04546377,-0.03862305,0.0015540555,0.0071577085,0.020040372,0.036925778,0.029104812,0.0132827945,-0.107043594,0.01852958,0.029878838,0.022493517,-0.014804282,-0.004237807,0.009568705,0.00784538,-0.072402604,-0.016833521,0.028639035,-0.01627254,-0.021198895,0.0029930559,0.022778021,-0.015627181,-0.0033616035,0.071218684,-0.015262994,-0.10250901,-0.0020695645,-0.036439225,-0.0069441283,-0.04960896,-0.012271773,0.028486863,0.05981711,-0.02299749,0.00985628,-0.01384696,0.0100355465,0.026329199,-0.015577187,0.027275965,-0.010776409,-0.033911444,-0.03296954,-0.078865,0.03220576,-0.013571237,-0.0019777275,0.022126189,-0.04235654,-0.02092788,0.09689952,0.051541816,0.020435747,0.022501897,0.018774781,-0.013723081,-0.023056464,0.013957139,-0.017759273,0.009288919,-0.091076516,0.055192713,0.009520295,-0.028882628,0.027038833,0.052017257,-0.03128131,-0.021672437,0.023145016,-0.059318632,-0.004515608,0.011910352,-0.011477609,0.01309707,-0.009831065,-0.034980353,0.009606157,-0.01994481,-0.013557006,0.029904185,0.044127822,0.027724477,-0.060295537,0.018798444,-0.0020928718,0.007569384,-0.036186844,0.005872834,-0.014641683,-0.0032251766,-0.022511672,0.0629932,0.04988445,-0.00077525317,-0.017013745,-0.0043008346,0.028987736,-0.0043742894,0.045512557,0.0082594855,-0.082569286,-0.010844901,-0.010817143,-0.027214594,-0.0044392566,0.05630547,0.0060081244,-0.019999515,-0.0095417015,-0.017677091,0.049479645,0.06770379,-0.009159886,-0.040117633,0.023651555,0.047912724,0.056500115,0.021024976,-0.014629467,-0.039146997,-0.010858068,0.03107423,0.10980052,-0.04106985,-0.021731436,0.07066263,-0.008242931,-0.045947798,0.042014185,-0.047510035,0.0064428374,-0.010548176,0.000010354073,-0.028542453,-0.023624012,-0.06434043,0.014106878,0.0131347105,0.07513478,-0.0075599793,0.01574876,-0.038697496,0.08632555,-0.051019404,0.003919646,0.031171538,0.025889423,0.011629111,0.0064080334,-0.04628214,0.006555972,-0.033805303,-0.037610102,0.010392073,-0.1256088,0.012700037,0.012758032,-0.02190892,0.015808214,-0.030578563,0.0073867827,-0.0077743884,0.022535944,0.003978741,-0.04370161,0.034711864,0.0006157731,0.0000000000000000000000000000000021943142,0.006185772,-0.05345853,0.04272408,-0.0247175,-0.017734552,-0.05928212,-0.030168809,0.018338332,-0.07523395,0.007613947,-0.008389085,-0.06015112,0.018915698,0.030033203,0.0186327,0.03689281,-0.035959523,-0.0034225495,0.003539659,0.0029228574,-0.005714868,-0.027298123,0.04814699,0.022140006,-0.0053261253,0.0505255,0.021097604,-0.034733344,0.025638448,0.0070095095,-0.0036155032,-0.013876542,0.024071245,0.046420123,-0.022584684,-0.011204314,-0.018801687,0.019528901,0.01197902,-0.084183425,-0.0023818803,0.007637708,0.015448257,-0.03896311,-0.017375687,-0.013326956,-0.008637424,0.072262466,-0.014326543,-0.027463159,-0.0046262117,-0.015684368,0.017562244,0.014260576,0.0126694385,0.061732735,-0.014603865,-0.059851352,-0.0003544833,-0.04176204,-0.029519131,0.035264056,-0.08270178,-0.007358092,-0.010077996,-0.0095289815,-0.027766364,0.0014643805,-0.029814469,-0.06463796,0.0656989,-0.074057736,0.015683228,0.015518605,0.015523905,0.030121315,-0.014840049,0.0039887684,-0.002194321,-0.013398842,-0.028889814,0.0576887,-0.0683886,0.030562734,-0.041627575,0.12759495,0.0065128063,-0.010678113,-0.0058187614,-0.008060728,-0.01763387,0.0011476054,-0.01917683,0.0025768315,-0.037197717,0.021147091,-0.0099801095,0.038394224,0.005960778,0.004520945,-0.005293453,-0.016030664,-0.05981525,-0.010780415,-0.018268123,-0.01388364,-0.018065583,-0.012215754,0.024128878,-0.0070569,0.027151046,-0.008134592,0.040488742,0.016176596,-0.008965093,-0.0054786587,0.09017081,-0.07191874,-0.018426714,0.06410183,-0.00902226,0.006911044,-0.048737586,0.013310079,0.05526136,-0.022550082,-0.06732778,0.07058923,-0.004299804,0.028841516,-0.028057793,-0.016158303,0.03535495,0.0028918488,-0.013917152,-0.0024057683,0.003768504,-0.011944575,-0.0064126956,-0.030020064,0.0026400948,0.0079075415,-0.0059171654,0.03530877,-0.027216263,-0.031498365,0.07942978,0.014092781,0.02192034,0.011844559,-0.04448484,0.012918128,-0.031016659,-0.02287978,-0.037731156,-0.0107439365,0.083177045,0.003730115,0.06401016,0.06926923,-0.006223299,-0.005915602,-0.04301891,-0.051654033,-0.05909956,0.021006368,0.0156718,-0.054006595,0.017506393,-0.061465666,-0.010994985,-0.035443902,-0.09785101,0.016555531,0.015038646,-0.00008615076,0.06428011,-0.0028268506,0.013043133,0.013385074,-0.009035897,0.03220018,-0.053479914,-0.028732825,-0.08202578,-0.025013523,0.024732003,0.058447663,-0.04123997,-0.026876403,-0.02872386,-0.021600695,0.027204804,-0.021732323,-0.04079403,0.002196618,0.11624145,0.07228676,-0.03246866,0.0057272683,-0.039232213,-0.027007297,0.028902365,-0.03851736,0.014882066,-0.018517954,-0.05772798,-0.012651224,-0.004279846,0.030413093,-0.0725985,0.027345056,0.03150099,0.047890432,0.01595836,-0.0024272834,0.009781816,0.004271278,0.03276759,0.019946035,0.016230986,0.035096224,0.0061147525,0.0037215108,0.0036886723,-0.01804743,-0.024052592,-0.020760896,0.0017854697,-0.022679156,-0.07487512,-0.03962129,-0.01519123,0.06230688,0.032761052,-0.001040691,0.028933955,-0.025039526,0.023194054,-0.02746612,-0.0751072,0.027643258,0.03461962,0.031516194,0.014787525,0.016682243,-0.056967963,0.079286605,0.01325139,0.021973861,-0.0042198896,0.016470337,-0.024541525,-0.023459064,0.023577321,-0.02537537,-0.0020904797,-0.04105016,-0.034043014,0.006171314,0.0350141,-0.019133007,0.02374956,0.060252313,-0.018008688,-0.057045206,0.038121223,0.051368166,-0.00507607,0.011559096,-0.017538754,0.008955292,0.012098412"
	strFloats := strings.Split(vectorStr, ",")
	floats := make([]float32, len(strFloats))
	for i, strFloat := range strFloats {
		f, err := strconv.ParseFloat(strFloat, 32)
		if err != nil {
			t.Fatalf("Failed to parse float: %v", err)
		}
		floats[i] = float32(f)
	}
	return floats
}
