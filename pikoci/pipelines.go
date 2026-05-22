package pikoci

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/awalterschulze/gographviz"
	"github.com/google/uuid"
	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/pipeline"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/restype"
	"github.com/xescugc/pikoci/pikoci/scheduler"
	"github.com/xescugc/pikoci/pikoci/sectype"
	"github.com/xescugc/pikoci/pikoci/unitwork"
	"github.com/xescugc/pikoci/pikoci/utils"
)

func (q *PikoCI) CreatePipeline(ctx context.Context, tc, pn string, rpp []byte, vars map[string]interface{}) (*pipeline.Pipeline, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}

	pCan := utils.Canonicalize(pn)
	if !utils.ValidateCanonical(pCan) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	}

	pp, err := q.readPipeline(ctx, rpp, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to read Pipeline config: %w", err)
	}

	pp.Name = pn
	pp.Canonical = pCan
	pp.Raw = rpp

	var cp *pipeline.Pipeline
	err = q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		_, err := uow.Pipelines().Create(ctx, tc, *pp)
		if err != nil {
			return fmt.Errorf("failed to create Pipeline %q: %w", pn, err)
		}

		for _, j := range pp.Jobs {
			if !utils.ValidateCanonical(j.Name) {
				return fmt.Errorf("invalid Job Name format %q", j.Name)
			}
			_, err = uow.Jobs().Create(ctx, tc, pCan, j)
			if err != nil {
				return fmt.Errorf("failed to create Job %q: %w", j.Name, err)
			}
		}

		for _, rt := range pp.ResourceTypes {
			if !utils.ValidateCanonical(rt.Name) {
				return fmt.Errorf("invalid ResourceType Name format %q", rt.Name)
			}
			_, err = uow.ResourceTypes().Create(ctx, tc, pCan, rt)
			if err != nil {
				return fmt.Errorf("failed to create ResourceType %q: %w", rt.Name, err)
			}
		}

		for _, r := range pp.Resources {
			if !utils.ValidateCanonical(r.Name) {
				return fmt.Errorf("invalid Resource Name format %q", r.Name)
			}
			spec := r.CheckInterval
			if spec == "" {
				spec = "@every 1m"
			}
			if err := scheduler.ValidateCheckInterval(spec); err != nil {
				return fmt.Errorf("invalid check_interval for resource %q: %w", r.Name, err)
			}
			nextCheck, err := scheduler.ComputeNextCheck(spec, time.Now())
			if err != nil {
				return fmt.Errorf("failed to compute next check for resource %q: %w", r.Name, err)
			}
			r.NextCheck = nextCheck
			r.WebhookToken = uuid.New().String()
			_, err = uow.Resources().Create(ctx, tc, pCan, r)
			if err != nil {
				return fmt.Errorf("failed to create Resource %q: %w", r.Name, err)
			}
		}

		for _, ru := range pp.Runners {
			if !utils.ValidateCanonical(ru.Name) {
				return fmt.Errorf("invalid Runner Name format %q", ru.Name)
			}
			_, err = uow.Runners().Create(ctx, tc, pCan, ru)
			if err != nil {
				return fmt.Errorf("failed to create Runner %q: %w", ru.Name, err)
			}
		}

		for _, st := range pp.SecretTypes {
			if !utils.ValidateCanonical(st.Name) {
				return fmt.Errorf("invalid SecretType Name format %q", st.Name)
			}
			_, err = uow.SecretTypes().Create(ctx, tc, pCan, st)
			if err != nil {
				return fmt.Errorf("failed to create SecretType %q: %w", st.Name, err)
			}
		}

		cp, err = uow.Pipelines().Find(ctx, tc, pCan)
		if err != nil {
			return fmt.Errorf("failed to get Pipeline: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cp, nil
}

func (q *PikoCI) UpdatePipeline(ctx context.Context, tc, pCan string, rpp []byte, vars map[string]interface{}) (*pipeline.Pipeline, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}

	pp, err := q.readPipeline(ctx, rpp, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to read Pipeline config: %w", err)
	}

	// Fetch existing pipeline to preserve the display name
	existingPP, err := q.Pipelines.Find(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("failed to find Pipeline %q: %w", pCan, err)
	}

	pp.Name = existingPP.Name
	pp.Canonical = pCan
	pp.Raw = rpp

	var up *pipeline.Pipeline
	err = q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		err := uow.Pipelines().Update(ctx, tc, pCan, *pp)
		if err != nil {
			return fmt.Errorf("failed to update Pipeline %q: %w", pCan, err)
		}

		dbpp, err := uow.Pipelines().Find(ctx, tc, pCan)
		if err != nil {
			return fmt.Errorf("failed to get Pipeline %q: %w", pCan, err)
		}

		dbjbs := make(map[string]struct{})
		for _, j := range dbpp.Jobs {
			dbjbs[j.Name] = struct{}{}
		}
		for _, j := range pp.Jobs {
			if !utils.ValidateCanonical(j.Name) {
				return fmt.Errorf("invalid Job Name format %q", j.Name)
			}
			if _, ok := dbjbs[j.Name]; ok {
				delete(dbjbs, j.Name)
				err = uow.Jobs().Update(ctx, tc, pCan, j.Name, j)
				if err != nil {
					return fmt.Errorf("failed to update Job %q: %w", j.Name, err)
				}
			} else {
				_, err = uow.Jobs().Create(ctx, tc, pCan, j)
				if err != nil {
					return fmt.Errorf("failed to create Job %q: %w", j.Name, err)
				}
			}
		}
		for jn := range dbjbs {
			err = uow.Jobs().Delete(ctx, tc, pCan, jn)
			if err != nil {
				return fmt.Errorf("failed to delete Job %q: %w", jn, err)
			}
		}

		dbrts := make(map[string]struct{})
		for _, rt := range dbpp.ResourceTypes {
			dbrts[rt.Name] = struct{}{}
		}
		for _, rt := range pp.ResourceTypes {
			if !utils.ValidateCanonical(rt.Name) {
				return fmt.Errorf("invalid ResourceType Name format %q", rt.Name)
			}
			if _, ok := dbrts[rt.Name]; ok {
				delete(dbrts, rt.Name)
				err = uow.ResourceTypes().Update(ctx, tc, pCan, rt.Name, rt)
				if err != nil {
					return fmt.Errorf("failed to update ResourceType %q: %w", rt.Name, err)
				}
			} else {
				_, err = uow.ResourceTypes().Create(ctx, tc, pCan, rt)
				if err != nil {
					return fmt.Errorf("failed to create ResourceType %q: %w", rt.Name, err)
				}
			}
		}
		for rt := range dbrts {
			err = uow.ResourceTypes().Delete(ctx, tc, pCan, rt)
			if err != nil {
				return fmt.Errorf("failed to delete ResourceType %q: %w", rt, err)
			}
		}

		dbrs := make(map[string]resource.Resource)
		for _, r := range dbpp.Resources {
			dbrs[r.Canonical] = r
		}
		for _, r := range pp.Resources {
			if !utils.ValidateCanonical(r.Name) {
				return fmt.Errorf("invalid Resource Name format %q", r.Name)
			}
			spec := r.CheckInterval
			if spec == "" {
				spec = "@every 1m"
			}
			if err := scheduler.ValidateCheckInterval(spec); err != nil {
				return fmt.Errorf("invalid check_interval for resource %q: %w", r.Name, err)
			}
			if dbr, ok := dbrs[r.Canonical]; ok {
				delete(dbrs, r.Canonical)
				if dbr.CheckInterval != r.CheckInterval {
					nextCheck, err := scheduler.ComputeNextCheck(spec, time.Now())
					if err != nil {
						return fmt.Errorf("failed to compute next check for resource %q: %w", r.Canonical, err)
					}
					r.NextCheck = nextCheck
				} else {
					r.NextCheck = dbr.NextCheck
				}
				r.WebhookToken = dbr.WebhookToken
				err = uow.Resources().Update(ctx, tc, pCan, r.Canonical, r)
				if err != nil {
					return fmt.Errorf("failed to update Resource %q: %w", r.Canonical, err)
				}
			} else {
				nextCheck, err := scheduler.ComputeNextCheck(spec, time.Now())
				if err != nil {
					return fmt.Errorf("failed to compute next check for resource %q: %w", r.Canonical, err)
				}
				r.NextCheck = nextCheck
				r.WebhookToken = uuid.New().String()
				_, err = uow.Resources().Create(ctx, tc, pCan, r)
				if err != nil {
					return fmt.Errorf("failed to create Resource %q: %w", r.Canonical, err)
				}
			}
		}
		for rc := range dbrs {
			err = uow.Resources().Delete(ctx, tc, pCan, rc)
			if err != nil {
				return fmt.Errorf("failed to delete Resource %q: %w", rc, err)
			}
		}

		dbru := make(map[string]struct{})
		for _, ru := range dbpp.Runners {
			dbru[ru.Name] = struct{}{}
		}
		for _, ru := range pp.Runners {
			if !utils.ValidateCanonical(ru.Name) {
				return fmt.Errorf("invalid Resource Name format %q", ru.Name)
			}
			if _, ok := dbru[ru.Name]; ok {
				delete(dbru, ru.Name)
				err = uow.Runners().Update(ctx, tc, pCan, ru.Name, ru)
				if err != nil {
					return fmt.Errorf("failed to update Runner %q: %w", ru.Name, err)
				}
			} else {
				_, err = uow.Runners().Create(ctx, tc, pCan, ru)
				if err != nil {
					return fmt.Errorf("failed to create Runner %q: %w", ru.Name, err)
				}
			}
		}
		for run := range dbru {
			err = uow.Runners().Delete(ctx, tc, pCan, run)
			if err != nil {
				return fmt.Errorf("failed to delete Runner %q: %w", run, err)
			}
		}

		dbsts := make(map[string]struct{})
		for _, st := range dbpp.SecretTypes {
			dbsts[st.Name] = struct{}{}
		}
		for _, st := range pp.SecretTypes {
			if !utils.ValidateCanonical(st.Name) {
				return fmt.Errorf("invalid SecretType Name format %q", st.Name)
			}
			if _, ok := dbsts[st.Name]; ok {
				delete(dbsts, st.Name)
				err = uow.SecretTypes().Update(ctx, tc, pCan, st.Name, st)
				if err != nil {
					return fmt.Errorf("failed to update SecretType %q: %w", st.Name, err)
				}
			} else {
				_, err = uow.SecretTypes().Create(ctx, tc, pCan, st)
				if err != nil {
					return fmt.Errorf("failed to create SecretType %q: %w", st.Name, err)
				}
			}
		}
		for stn := range dbsts {
			err = uow.SecretTypes().Delete(ctx, tc, pCan, stn)
			if err != nil {
				return fmt.Errorf("failed to delete SecretType %q: %w", stn, err)
			}
		}

		up, err = uow.Pipelines().Find(ctx, tc, pCan)
		if err != nil {
			return fmt.Errorf("failed to get Pipeline: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return up, nil
}

func (q *PikoCI) ListPipelines(ctx context.Context, tc string) ([]*pipeline.Pipeline, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}

	pps, err := q.Pipelines.Filter(ctx, tc)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Pipelines: %w", err)
	}

	lastBuilds, err := q.Builds.LastBuildAtByPipeline(ctx, tc)
	if err != nil {
		return nil, fmt.Errorf("failed to get last build timestamps: %w", err)
	}

	for _, pp := range pps {
		if t, ok := lastBuilds[pp.ID]; ok {
			t := t
			pp.LastBuildAt = &t
		}
	}

	return pps, nil
}

func (q *PikoCI) GetPipeline(ctx context.Context, tc, pCan string) (*pipeline.Pipeline, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}

	pp, err := q.Pipelines.Find(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("failed to get Pipeline %q: %w", pCan, err)
	}

	return pp, nil
}

var (
	jobColors = map[build.Status]string{
		build.Started:   `"#FFA300"`,
		build.Failed:    `"#FF004D"`,
		build.Succeeded: `"#00A83A"`,
		build.Cancelled: `"#AB5236"`,
	}
	jobBorderColors = map[build.Status]string{
		build.Started:   `"#CC8200"`,
		build.Failed:    `"#CC003E"`,
		build.Succeeded: `"#008030"`,
		build.Cancelled: `"#8A3F2B"`,
	}
	colorResource       = `"#83769C"`
	colorResourceBorder = `"#5F574F"`
	colorDefault        = `"#83769C"`
	colorDefaultBorder  = `"#5F574F"`
	colorError          = `"#FF004D"`
)

func (q *PikoCI) GetPipelineImage(ctx context.Context, tc, pCan, format string) ([]byte, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}
	if format == "" {
		format = "dot"
	}
	if strings.Contains(format, ".") {
		format = strings.Split(format, ".")[1]
	}

	if format != "dot" {
		return nil, fmt.Errorf("invalid image format %q", format)
	}

	pp, err := q.GetPipeline(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("failed to get Pipeline %q: %w", pCan, err)
	}

	img, err := q.generateImage(ctx, tc, pp)
	if err != nil {
		return nil, fmt.Errorf("failed to generate image: %w", err)
	}

	return img, err
}

func (q *PikoCI) generateImage(ctx context.Context, tc string, pp *pipeline.Pipeline) ([]byte, error) {
	var (
		pn  = fmt.Sprintf(`"%s"`, pp.Canonical)
		err error
	)

	graph := gographviz.NewGraph()
	graph.SetName(pn)
	graph.SetStrict(true)
	graph.AddAttr(pn, string(gographviz.RankDir), "LR")

	// Collect resources referenced by get steps
	referencedResources := make(map[string]bool)
	for _, j := range pp.Jobs {
		for _, g := range j.GetSteps() {
			referencedResources[g.ResourceCanonical()] = true
		}
	}

	resourceBorders := make(map[string]string)
	// Print all the resources
	for _, r := range pp.Resources {
		borderColor := colorResourceBorder
		if r.Logs != "" {
			borderColor = colorError
		}
		resourceBorders[r.Canonical] = borderColor
		if !referencedResources[r.Canonical] {
			continue
		}
		vurl := fmt.Sprintf(`"/teams/%s/pipelines/%s/resources/%s/versions"`, tc, pp.Canonical, r.Canonical)
		err = graph.AddNode(pn, fmt.Sprintf(`"%s"`, r.Canonical), map[string]string{
			string(gographviz.Margin):    "0.2",
			string(gographviz.Shape):     "cds",
			string(gographviz.FillColor): colorResource,
			string(gographviz.Style):     "filled",
			string(gographviz.FontColor): "white",
			string(gographviz.URL):       vurl,
			string(gographviz.Color):     borderColor,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add node to Graph: %w", err)
		}
	}

	// Print all the Jobs and the connection to resources
	for i, j := range pp.Jobs {
		builds, err := q.Builds.Filter(ctx, tc, pp.Canonical, j.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to filter builds from Job %q: %w", j.Name, err)
		}
		slices.Reverse(builds)
		color := colorDefault
		borderColor := colorDefaultBorder

		// cb: latest main build (no retry suffix) for fill color
		// rb: any running build (including retries) for dashed outline
		var (
			cb *build.Build
			rb *build.Build
		)
		for _, b := range builds {
			if b.Status == build.Started && rb == nil {
				rb = b
			}
			if !strings.Contains(b.BuildNumber, ".") && cb == nil {
				cb = b
			}
			if cb != nil && rb != nil {
				break
			}
		}

		// If the latest main build is running, use the previous main build for color
		if cb != nil && cb.Status == build.Started {
			for _, b := range builds {
				if b != cb && !strings.Contains(b.BuildNumber, ".") {
					if c, ok := jobColors[b.Status]; ok {
						color = c
					}
					if c, ok := jobBorderColors[b.Status]; ok {
						borderColor = c
					}
					break
				}
			}
		} else if cb != nil {
			if c, ok := jobColors[cb.Status]; ok {
				color = c
			}
			if c, ok := jobBorderColors[cb.Status]; ok {
				borderColor = c
			}
		}

		style := "invis"
		if rb != nil {
			style = `"dashed,bold"`
		}

		jg := fmt.Sprintf("cluster_%d", i)
		graph.AddSubGraph(pn, jg, map[string]string{
			string(gographviz.Style): style,
			string(gographviz.Color): jobBorderColors[build.Started],
		})

		burl := fmt.Sprintf(`"/teams/%s/pipelines/%s/jobs/%s/builds"`, tc, pp.Canonical, j.Name)
		quotedJobName := fmt.Sprintf(`"%s"`, j.Name)
		err = graph.AddNode(jg, quotedJobName, map[string]string{
			string(gographviz.Margin):    "0.5",
			string(gographviz.Shape):     "rectangle",
			string(gographviz.FillColor): color,
			string(gographviz.Style):     "filled",
			string(gographviz.FontColor): "white",
			string(gographviz.Color):     borderColor,
			string(gographviz.URL):       burl,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add node to Graph: %w", err)
		}
		// Draw resource→job edges for get steps without passed constraints
		for _, g := range j.GetSteps() {
			if len(g.Passed) == 0 {
				rCan := fmt.Sprintf(`"%s.%s"`, g.Type, g.Name)
				opt := make(map[string]string)
				if g.Trigger {
					opt[string(gographviz.Style)] = "solid"
				} else {
					opt[string(gographviz.Style)] = "dashed"
				}
				err = graph.AddEdge(rCan, quotedJobName, false, opt)
				if err != nil {
					return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
				}
			}
		}
		// Draw job→resource edges for all put steps (plan + hooks).
		// Each job gets its own output resource node to avoid all jobs
		// pointing to a single shared resource box.
		for _, p := range j.AllPutSteps() {
			rCan := fmt.Sprintf("%s.%s", p.Type, p.Name)
			nn := fmt.Sprintf(`"%s-%s-out"`, j.Name, rCan)
			vurl := fmt.Sprintf(`"/teams/%s/pipelines/%s/resources/%s/versions"`, tc, pp.Canonical, rCan)
			border := resourceBorders[rCan]
			err = graph.AddNode(pn, nn, map[string]string{
				string(gographviz.Label):     fmt.Sprintf(`"%s"`, rCan),
				string(gographviz.Margin):    "0.2",
				string(gographviz.Shape):     "cds",
				string(gographviz.FillColor): colorResource,
				string(gographviz.Style):     "filled",
				string(gographviz.FontColor): "white",
				string(gographviz.URL):       vurl,
				string(gographviz.Color):     border,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to add node to Graph: %w", err)
			}
			err = graph.AddEdge(quotedJobName, nn, false, map[string]string{
				string(gographviz.Style): "solid",
			})
			if err != nil {
				return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
			}
		}
	}

	// Now we print all the jobs interconnections depending on resources
	for _, j := range pp.Jobs {
		quotedJobName := fmt.Sprintf(`"%s"`, j.Name)
		for _, g := range j.GetSteps() {
			if len(g.Passed) != 0 {
				for _, p := range g.Passed {
					nn := fmt.Sprintf(`"%s-%s-%s"`, p, g.Name, j.Name)
					rCan := fmt.Sprintf("%s.%s", g.Type, g.Name)
					vurl := fmt.Sprintf(`"/teams/%s/pipelines/%s/resources/%s/versions"`, tc, pp.Canonical, rCan)
					border := resourceBorders[rCan]
					err = graph.AddNode(pn, nn, map[string]string{
						string(gographviz.Label):     fmt.Sprintf(`"%s"`, rCan),
						string(gographviz.Margin):    "0.2",
						string(gographviz.Shape):     "cds",
						string(gographviz.FillColor): colorResource,
						string(gographviz.Style):     "filled",
						string(gographviz.FontColor): "white",
						string(gographviz.URL):       vurl,
						string(gographviz.Color):     border,
					})
					if err != nil {
						return nil, fmt.Errorf("failed to add node to Graph: %w", err)
					}
					quotedPassedName := fmt.Sprintf(`"%s"`, p)
					err = graph.AddEdge(quotedPassedName, nn, false, nil)
					if err != nil {
						return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
					}
					err = graph.AddEdge(nn, quotedJobName, false, nil)
					if err != nil {
						return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
					}
				}
			}
		}
	}

	str := graph.String()
	return []byte(str), nil
}

func (q *PikoCI) CreatePipelineImage(ctx context.Context, tc string, pipeline []byte, vars map[string]interface{}, format string) ([]byte, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}

	pp, err := q.readPipeline(ctx, pipeline, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to read Pipeline: %w", err)
	}

	pp.Name = "pikoci"
	pp.Canonical = "pikoci"

	img, err := q.generateImage(ctx, tc, pp)
	if err != nil {
		return nil, fmt.Errorf("failed to generate image: %w", err)
	}

	return img, err
}

func sanitizePipelineForPublic(pp *pipeline.Pipeline) *pipeline.Pipeline {
	cp := *pp
	cp.Raw = nil
	rs := make([]resource.Resource, len(cp.Resources))
	for i, r := range cp.Resources {
		rs[i] = sanitizeResourceForPublic(r)
	}
	cp.Resources = rs
	rts := make([]restype.ResourceType, len(cp.ResourceTypes))
	for i, rt := range cp.ResourceTypes {
		rts[i] = restype.ResourceType{
			ID:     rt.ID,
			Name:   rt.Name,
			Source: rt.Source,
		}
	}
	cp.ResourceTypes = rts
	sts := make([]sectype.SecretType, len(cp.SecretTypes))
	for i, st := range cp.SecretTypes {
		sts[i] = sectype.SecretType{
			ID:     st.ID,
			Name:   st.Name,
			Source: st.Source,
		}
	}
	cp.SecretTypes = sts
	return &cp
}

func sanitizeResourceForPublic(r resource.Resource) resource.Resource {
	r.Params = nil
	r.WebhookToken = ""
	r.Logs = ""
	return r
}

func (q *PikoCI) SetPipelinePublic(ctx context.Context, tc, pCan string, public bool) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}

	return q.Pipelines.SetPublic(ctx, tc, pCan, public)
}

func (q *PikoCI) GetPublicPipeline(ctx context.Context, tc, pCan string) (*pipeline.Pipeline, error) {
	pp, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}
	return sanitizePipelineForPublic(pp), nil
}

func (q *PikoCI) GetPublicPipelineImage(ctx context.Context, tc, pCan, format string) ([]byte, error) {
	pp, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	if format == "" {
		format = "dot"
	}
	if strings.Contains(format, ".") {
		format = strings.Split(format, ".")[1]
	}
	if format != "dot" {
		return nil, fmt.Errorf("invalid image format %q", format)
	}

	return q.generateImage(ctx, tc, pp)
}

func (q *PikoCI) GetPublicPipelineJob(ctx context.Context, tc, pCan, jn string) (*job.Job, error) {
	_, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	return q.GetPipelineJob(ctx, tc, pCan, jn)
}

func (q *PikoCI) ListPublicJobBuilds(ctx context.Context, tc, pCan, jn string) ([]*build.Build, error) {
	_, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	builds, err := q.ListJobBuilds(ctx, tc, pCan, jn)
	if err != nil {
		return nil, err
	}
	for _, b := range builds {
		for i, s := range b.Steps {
			if s.Type == "secret" {
				b.Steps[i].Logs = ""
			}
		}
	}
	return builds, nil
}

func (q *PikoCI) GetPublicPipelineResource(ctx context.Context, tc, pCan, rCan string) (*resource.Resource, error) {
	_, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	r, err := q.GetPipelineResource(ctx, tc, pCan, rCan)
	if err != nil {
		return nil, err
	}

	sr := sanitizeResourceForPublic(*r)
	return &sr, nil
}

func (q *PikoCI) ListPublicResourceVersions(ctx context.Context, tc, pCan, rCan string) ([]*resource.Version, error) {
	_, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	return q.ListResourceVersions(ctx, tc, pCan, rCan)
}

func (q *PikoCI) DeletePipeline(ctx context.Context, tc, pCan string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}

	return q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		err := uow.Pipelines().Delete(ctx, tc, pCan)
		if err != nil {
			return fmt.Errorf("failed to delete Pipeline %q: %w", pCan, err)
		}

		return nil
	})
}
