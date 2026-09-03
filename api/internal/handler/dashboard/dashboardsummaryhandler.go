package dashboard

import (
	"net/http"

	"cscan/api/internal/logic"
	"cscan/api/internal/svc"
	"cscan/pkg/response"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// DashboardSummaryHandler Dashboard 统一汇总接口
func DashboardSummaryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewDashboardSummaryLogic(r.Context(), svcCtx)
		resp, err := l.DashboardSummary()
		if err != nil {
			response.Error(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}
