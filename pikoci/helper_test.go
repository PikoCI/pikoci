package pikoci_test

import (
	"context"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"go.uber.org/mock/gomock"
)

type MockService struct {
	Topic             *mock.Topic // used as JobTopic in tests
	CheckTopic        *mock.Topic
	Users             *mock.UserRepository
	Teams             *mock.TeamRepository
	Pipelines         *mock.PipelineRepository
	Jobs              *mock.JobRepository
	Resources         *mock.ResourceRepository
	ResourceTypes     *mock.ResourceTypeRepository
	Builds            *mock.BuildRepository
	Runners           *mock.RunnerRepository
	SecretTypes       *mock.SecretTypeRepository
	Triggers          *mock.TriggerRepository
	NotificationTypes *mock.NotificationTypeRepository
	Notifications     *mock.NotificationRepository
	Workers           *mock.WorkerRepository

	S pikoci.Service
	P *pikoci.PikoCI
}

func newService(ctrl *gomock.Controller) MockService {
	ur := mock.NewUserRepository(ctrl)
	tr := mock.NewTeamRepository(ctrl)
	pr := mock.NewPipelineRepository(ctrl)
	jr := mock.NewJobRepository(ctrl)
	rr := mock.NewResourceRepository(ctrl)
	rtr := mock.NewResourceTypeRepository(ctrl)
	br := mock.NewBuildRepository(ctrl)
	rur := mock.NewRunnerRepository(ctrl)
	str := mock.NewSecretTypeRepository(ctrl)
	tgr := mock.NewTriggerRepository(ctrl)
	ntr := mock.NewNotificationTypeRepository(ctrl)
	nr := mock.NewNotificationRepository(ctrl)
	wr := mock.NewWorkerRepository(ctrl)
	t := mock.NewTopic(ctrl)
	ct := mock.NewTopic(ctrl)

	suow := unitwork.NewNoopStartUnitOfWork(unitwork.Repositories{
		UsersRepo:             ur,
		TeamsRepo:             tr,
		PipelinesRepo:         pr,
		JobsRepo:              jr,
		ResourcesRepo:         rr,
		ResourceTypesRepo:     rtr,
		BuildsRepo:            br,
		RunnersRepo:           rur,
		SecretTypesRepo:       str,
		NotificationTypesRepo: ntr,
		NotificationsRepo:     nr,
	})

	p := pikoci.New(context.TODO(), t, ct, ur, tr, pr, jr, rr, rtr, br, rur, str, tgr, wr, suow, []byte("test-secret"), nil, nil)
	return MockService{
		Topic:             t,
		CheckTopic:        ct,
		Users:             ur,
		Teams:             tr,
		Pipelines:         pr,
		Jobs:              jr,
		Resources:         rr,
		ResourceTypes:     rtr,
		Builds:            br,
		Runners:           rur,
		SecretTypes:       str,
		Triggers:          tgr,
		NotificationTypes: ntr,
		Notifications:     nr,
		Workers:           wr,

		S: p,
		P: p,
	}
}
