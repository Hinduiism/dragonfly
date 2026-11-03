package session

import (
	"github.com/df-mc/dragonfly/server/player/camera"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func (s *Session) RegisterCamera() *Delivery {
	return s.queueDelivery(&packet.CameraPresets{Presets: []protocol.CameraPreset{{Name: "minecraft:free"}}})
}
func (s *Session) SetCamera(view camera.View) *Delivery {
	set := protocol.CameraInstructionSet{Preset: 0, Position: protocol.Option(cameraVector(view.Position)), Facing: protocol.Option(cameraVector(view.Facing)), Default: protocol.Option(false)}
	if view.Duration > 0 {
		set.Ease = protocol.Option(protocol.CameraEase{Type: protocol.EasingTypeOutCubic, Duration: float32(view.Duration.Seconds())})
	}
	return s.queueDelivery(&packet.CameraInstruction{Set: protocol.Option(set)})
}
func (s *Session) FadeCamera(fade camera.Fade) *Delivery {
	return s.queueDelivery(&packet.CameraInstruction{Fade: protocol.Option(protocol.CameraInstructionFade{
		TimeData: protocol.Option(protocol.CameraFadeTimeData{FadeInDuration: float32(fade.In.Seconds()), WaitDuration: float32(fade.Hold.Seconds()), FadeOutDuration: float32(fade.Out.Seconds())}),
		Colour:   protocol.Option(fade.Colour),
	})})
}
func (s *Session) ClearCamera() *Delivery {
	return s.queueDelivery(&packet.CameraInstruction{Clear: protocol.Option(true)})
}
func (s *Session) ClearTitle() *Delivery {
	return s.queueDelivery(&packet.SetTitle{ActionType: packet.TitleActionClear})
}
func cameraVector(v mgl64.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{float32(v.X()), float32(v.Y()), float32(v.Z())}
}
