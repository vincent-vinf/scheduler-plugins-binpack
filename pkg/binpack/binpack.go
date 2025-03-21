package binpack

import (
	"context"
	"fmt"
	"math"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

type BinPack struct {
	handle framework.Handle
}

var _ = framework.ScorePlugin(&BinPack{})

const (
	GPUResourceName = "cmos.chinamobile.com/vgpu"
	Name            = "Binpack"
)

func (bp *BinPack) Name() string {
	return Name
}

// NormalizeScore 将分数规整到框架支持的分数区间
func (bp *BinPack) NormalizeScore(ctx context.Context, state *framework.CycleState, p *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	// Find highest and lowest scores.
	var highest int64 = math.MinInt64
	var lowest int64 = math.MaxInt64
	for _, nodeScore := range scores {
		if nodeScore.Score > highest {
			highest = nodeScore.Score
		}
		if nodeScore.Score < lowest {
			lowest = nodeScore.Score
		}
	}

	// Transform the highest to lowest score range to fit the framework's min to max node score range.
	oldRange := highest - lowest
	newRange := framework.MaxNodeScore - framework.MinNodeScore
	for i, nodeScore := range scores {
		if oldRange == 0 {
			scores[i].Score = framework.MinNodeScore
		} else {
			scores[i].Score = ((nodeScore.Score - lowest) * newRange / oldRange) + framework.MinNodeScore
		}
	}

	return nil
}

func useGPUResource(pod *v1.Pod) bool {
	for _, c := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
		if _, ok := c.Resources.Limits[GPUResourceName]; ok {
			return true
		}
	}
	return false
}

// Score invoked at the score extension point.
func (bp *BinPack) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	if !useGPUResource(pod) {
		return 0, nil
	}
	nodeInfo, err := bp.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("getting node %q from Snapshot: %v", nodeName, err))
	}
	return bp.score(nodeInfo)
}

func (bp *BinPack) score(nodeInfo *framework.NodeInfo) (int64, *framework.Status) {
	if _, ok := nodeInfo.Allocatable.ScalarResources[GPUResourceName]; !ok {
		return 0, framework.NewStatus(framework.UnschedulableAndUnresolvable)
	}
	// 剩余可分配量 = 总可分配量 - 已分配量
	rest := nodeInfo.Allocatable.ScalarResources[GPUResourceName] - nodeInfo.Requested.ScalarResources[GPUResourceName]
	score := -rest
	// 根据rest计算分数，剩余越多分数越低
	klog.Infof("node %s get score %d", nodeInfo.Node().Name, score)

	return score, nil
}

// ScoreExtensions of the Score plugin.
func (bp *BinPack) ScoreExtensions() framework.ScoreExtensions {
	return bp
}

func New(args runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &BinPack{
		handle: h,
	}, nil
}
