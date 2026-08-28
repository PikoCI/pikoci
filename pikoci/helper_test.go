package pikoci_test

import (
	"context"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/secret"
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
	Secrets           *mock.SecretRepository
	AuditLogs         *mock.AuditLogRepository
	// Audited collects every audit entry written through AuditLogs.
	Audited *[]auditlog.Entry

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
	secr := mock.NewSecretRepository(ctrl)

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
		SecretsRepo:           secr,
	})

	alr := mock.NewAuditLogRepository(ctrl)
	// Record what was audited so tests can assert on the action, rather than
	// only that some audit call happened.
	audited := &[]auditlog.Entry{}
	alr.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, e auditlog.Entry) error {
			*audited = append(*audited, e)
			return nil
		}).AnyTimes()

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
		Secrets:           secr,
		AuditLogs:         alr,
		Audited:           audited,

		S: p,
		P: p,
	}
}


// withSecretRepo rebuilds the noop unit of work so it hands out this secret
// repository. Tests that exercise the real store need the transactional
// accessor to return the same repository EnableSecretStore was given, rather
// than the mock the other repositories use.
func (ms MockService) withSecretRepo(sr secret.Repository) {
	ms.P.StartUoW = unitwork.NewNoopStartUnitOfWork(unitwork.Repositories{
		UsersRepo:             ms.Users,
		TeamsRepo:             ms.Teams,
		PipelinesRepo:         ms.Pipelines,
		JobsRepo:              ms.Jobs,
		ResourcesRepo:         ms.Resources,
		ResourceTypesRepo:     ms.ResourceTypes,
		BuildsRepo:            ms.Builds,
		RunnersRepo:           ms.Runners,
		SecretTypesRepo:       ms.SecretTypes,
		NotificationTypesRepo: ms.NotificationTypes,
		NotificationsRepo:     ms.Notifications,
		ApiTokensRepo:         ms.ApiTokens,
		SecretsRepo:           sr,
	})
}
