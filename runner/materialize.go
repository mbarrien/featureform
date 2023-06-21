// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package runner

import (
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	cfg "github.com/featureform/config"
	"github.com/featureform/helpers"
	"github.com/featureform/kubernetes"
	"github.com/featureform/logging"
	"github.com/featureform/metadata"
	"github.com/featureform/provider"
	pc "github.com/featureform/provider/provider_config"
	pt "github.com/featureform/provider/provider_type"
	"github.com/featureform/types"
)

const MAXIMUM_CHUNK_ROWS int64 = 16777216

var WORKER_IMAGE string = helpers.GetEnv("WORKER_IMAGE", "featureformcom/worker:latest")

type JobCloud string

const (
	KubernetesMaterializeRunner JobCloud = "KUBERNETES"
	LocalMaterializeRunner      JobCloud = "LOCAL"
)

type MaterializeRunner struct {
	Online   provider.OnlineStore
	Offline  provider.OfflineStore
	ID       provider.ResourceID
	VType    provider.ValueType
	IsUpdate bool
	Cloud    JobCloud
	Logger   *zap.SugaredLogger
}

func (m MaterializeRunner) Resource() metadata.ResourceID {
	return metadata.ResourceID{
		Name:    m.ID.Name,
		Variant: m.ID.Variant,
		Type:    provider.ProviderToMetadataResourceType[m.ID.Type],
	}
}

func (m MaterializeRunner) IsUpdateJob() bool {
	return m.IsUpdate
}

type WatcherMultiplex struct {
	CompletionList []types.CompletionWatcher
}

func (w WatcherMultiplex) Complete() bool {
	complete := true
	for _, completion := range w.CompletionList {
		complete = complete && completion.Complete()
	}
	return complete
}
func (w WatcherMultiplex) String() string {
	complete := 0
	for _, completion := range w.CompletionList {
		if completion.Complete() {
			complete += 1
		}
	}
	return fmt.Sprintf("%v complete out of %v", complete, len(w.CompletionList))
}
func (w WatcherMultiplex) Wait() error {
	for _, completion := range w.CompletionList {
		if err := completion.Wait(); err != nil {
			return err
		}
	}
	return nil
}
func (w WatcherMultiplex) Err() error {
	for _, completion := range w.CompletionList {
		if err := completion.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (m MaterializeRunner) Run() (types.CompletionWatcher, error) {
	m.Logger.Infow("Starting Materialization Runner", "name", m.ID.Name, "variant", m.ID.Variant)
	var materialization provider.Materialization
	var err error

	if m.IsUpdate {
		m.Logger.Infow("Updating Materialization", "name", m.ID.Name, "variant", m.ID.Variant)
		materialization, err = m.Offline.UpdateMaterialization(m.ID)
	} else {
		m.Logger.Infow("Creating Materialization", "name", m.ID.Name, "variant", m.ID.Variant)
		materialization, err = m.Offline.CreateMaterialization(m.ID)
	}
	if err != nil {
		return nil, err
	}
	// Create the vector similarity index prior to writing any values to the
	// inference store. This is currently only required for RediSearch, but other
	// vector databases allow for manual index configuration even if they support
	// autogeneration of indexes.
	vectorType, ok := m.VType.(provider.VectorType)
	if !ok {
		return nil, fmt.Errorf("cannot create index on non-vector type: %v", m.VType)
	}
	if vectorType.IsEmbedding {
		vectorStore, ok := m.Online.(provider.VectorStore)
		if !ok {
			return nil, fmt.Errorf("cannot create index on non-vector store: %v", m.Online)
		}
		m.Logger.Infow("Creating Index", "name", m.ID.Name, "variant", m.ID.Variant)
		_, err := vectorStore.CreateIndex(m.ID.Name, m.ID.Variant, vectorType)
		if err != nil {
			return nil, fmt.Errorf("create index error: %w", err)
		}
	}
	m.Logger.Infow("Creating Table", "name", m.ID.Name, "variant", m.ID.Variant)
	_, err = m.Online.CreateTable(m.ID.Name, m.ID.Variant, m.VType)
	_, exists := err.(*provider.TableAlreadyExists)
	if err != nil && !exists {
		return nil, fmt.Errorf("create table error: %w", err)
	}
	if exists && !m.IsUpdate {
		return nil, fmt.Errorf("table already exists despite being new job")
	}
	chunkSize := MAXIMUM_CHUNK_ROWS
	var numChunks int64
	m.Logger.Debugw("Getting number of rows", "name", m.ID.Name, "variant", m.ID.Variant)
	numRows, err := materialization.NumRows()
	if err != nil {
		return nil, fmt.Errorf("num rows: %w", err)
	}
	m.Logger.Debugw("Got materialization rows", "name", m.ID.Name, "variant", m.ID.Variant, "count", numRows)
	if numRows <= MAXIMUM_CHUNK_ROWS {
		chunkSize = numRows
		numChunks = 1
	} else if chunkSize == 0 {
		numChunks = 0
	} else if numRows > chunkSize {
		numChunks = numRows / chunkSize
		if chunkSize*numChunks < numRows {
			numChunks += 1
		}
	}
	m.Logger.Infow("Creating chunks", "name", m.ID.Name, "variant", m.ID.Variant, "count", numChunks)
	config := &MaterializedChunkRunnerConfig{
		OnlineType:     m.Online.Type(),
		OfflineType:    m.Offline.Type(),
		OnlineConfig:   m.Online.Config(),
		OfflineConfig:  m.Offline.Config(),
		MaterializedID: materialization.ID(),
		ResourceID:     m.ID,
		ChunkSize:      chunkSize,
		Logger:         m.Logger,
	}
	serializedConfig, err := config.Serialize()
	if err != nil {
		return nil, fmt.Errorf("could not serialize config : %w", err)
	}
	var cloudWatcher types.CompletionWatcher
	switch m.Cloud {
	case KubernetesMaterializeRunner:
		pandas_image := cfg.GetPandasRunnerImage()
		envVars := map[string]string{"NAME": string(COPY_TO_ONLINE), "CONFIG": string(serializedConfig), "PANDAS_RUNNER_IMAGE": pandas_image}
		kubernetesConfig := kubernetes.KubernetesRunnerConfig{
			JobPrefix: "materialize",
			EnvVars:   envVars,
			Image:     WORKER_IMAGE,
			NumTasks:  int32(numChunks),
			Resource:  metadata.ResourceID{Name: m.ID.Name, Variant: m.ID.Variant, Type: provider.ProviderToMetadataResourceType[m.ID.Type]},
		}
		kubernetesRunner, err := kubernetes.NewKubernetesRunner(kubernetesConfig)
		if err != nil {
			return nil, fmt.Errorf("kubernetes runner: %w", err)
		}
		cloudWatcher, err = kubernetesRunner.Run()
		if err != nil {
			return nil, fmt.Errorf("kubernetes run: %w", err)
		}
	case LocalMaterializeRunner:
		m.Logger.Infow("Making Local Runner", "name", m.ID.Name, "variant", m.ID.Variant)
		completionList := make([]types.CompletionWatcher, int(numChunks))
		for i := 0; i < int(numChunks); i++ {
			localRunner, err := Create(string(COPY_TO_ONLINE), serializedConfig)
			if err != nil {
				return nil, fmt.Errorf("local runner create: %w", err)
			}
			watcher, err := localRunner.Run()
			if err != nil {
				return nil, fmt.Errorf("local runner run: %w", err)
			}
			completionList[i] = watcher
		}
		cloudWatcher = WatcherMultiplex{completionList}
	default:
		return nil, fmt.Errorf("no valid job cloud set")
	}
	done := make(chan interface{})
	materializeWatcher := &SyncWatcher{
		ResultSync:  &ResultSync{},
		DoneChannel: done,
	}
	go func() {
		if err := cloudWatcher.Wait(); err != nil {
			materializeWatcher.EndWatch(fmt.Errorf("cloud watch: %w", err))
			return
		}
		materializeWatcher.EndWatch(nil)
	}()
	return materializeWatcher, nil
}

type MaterializedRunnerConfig struct {
	OnlineType    pt.Type
	OfflineType   pt.Type
	OnlineConfig  pc.SerializedConfig
	OfflineConfig pc.SerializedConfig
	ResourceID    provider.ResourceID
	VType         provider.ValueTypeJSONWrapper
	Cloud         JobCloud
	IsUpdate      bool
}

func (m *MaterializedRunnerConfig) Serialize() (Config, error) {
	config, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return config, nil
}

func (m *MaterializedRunnerConfig) Deserialize(config Config) error {
	err := json.Unmarshal(config, m)
	if err != nil {
		return err
	}
	return nil
}

func MaterializeRunnerFactory(config Config) (types.Runner, error) {
	runnerConfig := &MaterializedRunnerConfig{}
	if err := runnerConfig.Deserialize(config); err != nil {
		return nil, fmt.Errorf("failed to deserialize materialize runner config: %v", err)
	}
	onlineProvider, err := provider.Get(runnerConfig.OnlineType, runnerConfig.OnlineConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to configure online provider: %v", err)
	}
	offlineProvider, err := provider.Get(runnerConfig.OfflineType, runnerConfig.OfflineConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to configure offline provider: %v", err)
	}
	onlineStore, err := onlineProvider.AsOnlineStore()
	if err != nil {
		return nil, fmt.Errorf("failed to convert provider to online store: %v", err)
	}
	offlineStore, err := offlineProvider.AsOfflineStore()
	if err != nil {
		return nil, fmt.Errorf("failed to convert provider to offline store: %v", err)
	}
	return &MaterializeRunner{
		Online:   onlineStore,
		Offline:  offlineStore,
		ID:       runnerConfig.ResourceID,
		VType:    runnerConfig.VType.ValueType,
		IsUpdate: runnerConfig.IsUpdate,
		Cloud:    runnerConfig.Cloud,
		Logger:   logging.NewLogger("materializer"),
	}, nil
}
