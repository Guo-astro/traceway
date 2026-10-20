package debug

import (
	"fmt"
	"time"

	"github.com/tracewayapp/traceway/cli/test/evals/internal/scenarios"
	"github.com/tracewayapp/traceway/cli/test/evals/internal/seed"
)

func init() { scenarios.Register("debug/flutter-null-check", dbgSeedFlutterNullCheck) }

// dbgSeedFlutterNullCheck: the product page's hero carousel null-checks
// product.media, which the catalog omits for listings without photos. Such
// listings started appearing about a day ago (marketplace sellers), so the
// crash hits both app versions in the field and no app release lines up
// with it. Distractor: a steady trickle of host-lookup failures (offline
// phones) from the same API client.
func dbgSeedFlutterNullCheck(s *seed.Scenario) error {
	p := s.Project("shop-flutter", seed.Mobile, "flutter")
	const fw = "package:flutter/src/widgets/framework.dart"
	nullCheck := "_TypeError: Null check operator used on a null value\n" +
		"#0      ProductHeroCarousel.build (package:shop_app/features/product/widgets/product_hero_carousel.dart:24:32)\n" +
		"#1      StatelessElement.build (" + fw + ":5687:49)\n" +
		"#2      ComponentElement.performRebuild (" + fw + ":5617:15)\n" +
		"#3      Element.rebuild (" + fw + ":5333:7)\n" +
		"#4      ComponentElement._firstBuild (" + fw + ":5599:5)\n" +
		"#5      ComponentElement.mount (" + fw + ":5593:5)\n" +
		"#6      Element.inflateWidget (" + fw + ":4468:20)\n" +
		"#7      Element.updateChild (" + fw + ":3963:18)\n" +
		"#8      SliverMultiBoxAdaptorElement.updateChild (package:flutter/src/widgets/sliver.dart:1022:37)\n" +
		"#9      SliverMultiBoxAdaptorElement.createChild.<anonymous closure> (package:flutter/src/widgets/sliver.dart:892:20)\n" +
		"#10     BuildOwner.buildScope (" + fw + ":3045:19)\n" +
		"#11     SliverMultiBoxAdaptorElement.createChild (package:flutter/src/widgets/sliver.dart:884:12)\n" +
		"#12     RenderSliverMultiBoxAdaptor._createOrObtainChild.<anonymous closure> (package:flutter/src/rendering/sliver_multi_box_adaptor.dart:376:23)\n" +
		"#13     RenderObject.invokeLayoutCallback.<anonymous closure> (package:flutter/src/rendering/object.dart:2876:17)\n" +
		"#14     PipelineOwner._enableMutationsToDirtySubtrees (package:flutter/src/rendering/object.dart:1170:15)\n" +
		"#15     RenderObject.invokeLayoutCallback (package:flutter/src/rendering/object.dart:2875:14)\n" +
		"#16     RenderSliverMultiBoxAdaptor._createOrObtainChild (package:flutter/src/rendering/sliver_multi_box_adaptor.dart:365:5)\n" +
		"#17     RenderSliverMultiBoxAdaptor.addInitialChild (package:flutter/src/rendering/sliver_multi_box_adaptor.dart:461:5)\n" +
		"#18     RenderSliverList.performLayout (package:flutter/src/rendering/sliver_list.dart:58:12)\n" +
		"#19     RenderObject.layout (package:flutter/src/rendering/object.dart:2706:7)\n" +
		"#20     PipelineOwner.flushLayout (package:flutter/src/rendering/object.dart:1144:18)\n" +
		"#21     RendererBinding.drawFrame (package:flutter/src/rendering/binding.dart:622:23)\n" +
		"#22     WidgetsBinding.drawFrame (package:flutter/src/widgets/binding.dart:1241:13)\n" +
		"#23     RendererBinding._handlePersistentFrameCallback (package:flutter/src/rendering/binding.dart:482:5)\n" +
		"#24     SchedulerBinding._invokeFrameCallback (package:flutter/src/scheduler/binding.dart:1442:15)\n" +
		"#25     SchedulerBinding.handleDrawFrame (package:flutter/src/scheduler/binding.dart:1355:9)\n"
	devices := []string{"iPhone15,2", "iPhone16,1", "iPhone14,7", "Pixel 8", "SM-S921B", "SM-A546B", "Pixel 7a", "2312DRA50G"}

	for _, at := range dbgTimes(s, s.Ago(21*time.Hour), s.Ago(5*time.Minute), 72, true) {
		v := "4.3.0"
		if s.RNG.Float64() < 0.22 {
			v = "4.2.1"
		}
		c := seed.Crash{At: at, Stack: nullCheck, AppVersion: v, Device: dbgPick(s, devices)}
		if at.After(s.Ago(10 * time.Hour)) {
			id := 40000 + s.RNG.IntN(50000)
			c.Actions = []seed.Action{
				{Type: "navigation", At: at.Add(-time.Duration(900+s.RNG.IntN(1500)) * time.Millisecond), From: "/", To: fmt.Sprintf("/products/%d", id)},
				{Type: "network", At: at.Add(-time.Duration(500+s.RNG.IntN(600)) * time.Millisecond), Method: "GET", URL: fmt.Sprintf("https://api.shop.example/v1/products/%d", id), DurationMs: float64(90 + s.RNG.IntN(250)), Status: 200, RespBytes: 780 + s.RNG.IntN(300)},
			}
		}
		p.Crash(c)
	}
	for _, at := range dbgTimes(s, s.Ago(7*24*time.Hour), s.Ago(50*time.Minute), 22, false) {
		id := 40000 + s.RNG.IntN(50000)
		v := "4.3.0"
		if at.Before(s.Ago(9*24*time.Hour)) || s.RNG.Float64() < 0.3 {
			v = "4.2.1"
		}
		stack := fmt.Sprintf("ClientException: ClientException with SocketException: Failed host lookup: 'api.shop.example' (OS Error: No address associated with hostname, errno = 7), uri=https://api.shop.example/v1/products/%d\n", id) +
			"#0      IOClient.send (package:http/src/io_client.dart:154:7)\n<asynchronous suspension>\n" +
			"#1      BaseClient._sendUnstreamed (package:http/src/base_client.dart:93:32)\n<asynchronous suspension>\n" +
			"#2      ApiClient.product (package:shop_app/core/api_client.dart:14:17)\n<asynchronous suspension>\n"
		p.Crash(seed.Crash{At: at, Stack: stack, AppVersion: v, Device: dbgPick(s, devices)})
	}

	s.AfterFlush(func() error {
		hash, err := p.GroupMatching(8*24*time.Hour, "ProductHeroCarousel.build")
		if err != nil {
			return err
		}
		s.Expose("hash", hash)
		other, err := p.GroupMatching(8*24*time.Hour, "Failed host lookup")
		if err != nil {
			return err
		}
		s.Expose("otherHash", other)
		return nil
	})
	return nil
}
