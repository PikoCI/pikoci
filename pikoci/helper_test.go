package pikoci_test

import (
	"context"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"
)

// testHashPassword hashes with bcrypt cost 4 (fast for tests, vs cost 14 in production).
func testHashPassword(pass string) string {
	b, _ := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.MinCost)
	return string(b)
}

type MockService struct {
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
	ApiTokens         *mock.ApiTokenRepository
	AuditLogs         *mock.AuditLogRepository

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
	atr := mock.NewApiTokenRepository(ctrl)

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
		ApiTokensRepo:         atr,
	})

	alr := mock.NewAuditLogRepository(ctrl)
	alr.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	p := pikoci.New(context.TODO(), ur, tr, pr, jr, rr, rtr, br, rur, str, tgr, wr, atr, alr, nil, suow, []byte("test-secret"), nil, nil)
	return MockService{
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
		ApiTokens:         atr,
		AuditLogs:         alr,

		S: p,
		P: p,
	}
}
