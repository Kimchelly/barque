package rest

import (
	"fmt"
	"net/http"

	"github.com/evergreen-ci/barque/model"
	"github.com/evergreen-ci/gimlet"
	"github.com/mongodb/amboy"
	"github.com/mongodb/curator/repobuilder"
	"github.com/pkg/errors"
)

////////////////////////////////////////////////////////////////////////
//
// POST /repobuilder

func (s *Service) addRepobuilderJob(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conf, err := model.FindConfiguration(ctx, s.Environment)
	if err != nil {
		gimlet.WriteResponse(rw, gimlet.MakeJSONErrorResponder(gimlet.ErrorResponse{
			StatusCode: http.StatusInternalServerError,
			Message:    errors.Wrap(err, "problem finding system configuration").Error(),
		}))
		return
	}

	if conf.Flags.DisableRepobuilderJobSubmission {
		gimlet.WriteResponse(rw, gimlet.MakeJSONErrorResponder(gimlet.ErrorResponse{
			StatusCode: http.StatusServiceUnavailable,
			Message:    "job submission is currently disabled",
		}))
		return
	}

	opts := repobuilder.JobOptions{}
	if err = gimlet.GetJSON(r.Body, &opts); err != nil {
		gimlet.WriteResponse(rw, gimlet.MakeJSONErrorResponder(gimlet.ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Message:    errors.Wrap(err, "problem parsing input for repobuilder options").Error(),
		}))
		return
	}

	bucketConfig, err := conf.Repobuilder.GetBucketConfig(opts.Distro.Bucket)
	if err != nil {
		gimlet.WriteResponse(rw, gimlet.MakeJSONErrorResponder(gimlet.ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Message:    errors.Wrap(err, "barque service").Error(),
		}))
		return
	}

	opts.Key = bucketConfig.Key
	opts.Secret = bucketConfig.Secret
	opts.Token = bucketConfig.Token
	opts.Configuration.WorkSpace = conf.Repobuilder.Path

	job, err := repobuilder.NewRepoBuilderJob(opts)
	if err != nil {
		gimlet.WriteResponse(rw, gimlet.MakeJSONErrorResponder(gimlet.ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Message:    errors.Wrap(err, "problem building repobuilder job").Error(),
		}))
		return
	}

	if err = s.Environment.RemoteQueue().Put(ctx, job); err != nil {
		gimlet.WriteResponse(rw, gimlet.MakeJSONErrorResponder(gimlet.ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Message:    errors.Wrap(err, "problem building enquing job").Error(),
		}))
		return
	}

	gimlet.WriteJSON(rw, struct {
		ID     string   `json:"id"`
		Scopes []string `json:"scopes"`
	}{
		ID:     job.ID(),
		Scopes: job.Scopes(),
	})
}

////////////////////////////////////////////////////////////////////////
//
// GET /repobuilder/check/{job_id}

type checkRepobuilderJobOutput struct {
	JobStatus   amboy.JobStatusInfo `json:"job_status"`
	JobTiming   amboy.JobTimeInfo   `json:"job_timing"`
	QueueStatus amboy.QueueStats    `json:"queue_status"`
	HasErrors   bool                `json:"has_errors"`
	Error       string              `json:"error,omitempty"`
}

func (s *Service) checkRepobuilderJob(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	jobID := gimlet.GetVars(r)["job_id"]
	queue := s.Environment.RemoteQueue()
	job, ok := queue.Get(ctx, jobID)
	if !ok {
		gimlet.WriteResponse(rw, gimlet.MakeJSONErrorResponder(gimlet.ErrorResponse{
			StatusCode: http.StatusNotFound,
			Message:    fmt.Sprintf("job named '%s' was not found", jobID),
		}))
		return
	}
	output := &checkRepobuilderJobOutput{
		JobStatus:   job.Status(),
		JobTiming:   job.TimeInfo(),
		QueueStatus: queue.Stats(ctx),
		HasErrors:   job.Error() != nil,
	}
	if output.HasErrors {
		output.Error = job.Error().Error()
	}

	gimlet.WriteJSON(rw, output)
}
