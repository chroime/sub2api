package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ListPublic exposes active channel coverage for public groups only, independent
// of the authenticated console's available-channels feature switch.
func (h *AvailableChannelHandler) ListPublic(c *gin.Context) {
	channels, err := h.channelService.ListAvailable(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]userAvailableChannel, 0, len(channels))
	for _, channel := range channels {
		if channel.Status != service.StatusActive {
			continue
		}
		publicGroupIDs := make(map[int64]struct{}, len(channel.Groups))
		for _, group := range channel.Groups {
			if !group.IsExclusive {
				publicGroupIDs[group.ID] = struct{}{}
			}
		}
		sections := buildPlatformSections(channel, filterUserVisibleGroups(channel.Groups, publicGroupIDs))
		if len(sections) == 0 {
			continue
		}
		// Channel descriptions can contain operational notes. Publish only names
		// and platform coverage; price data comes from the billing-backed endpoint.
		for i := range sections {
			for j := range sections[i].SupportedModels {
				sections[i].SupportedModels[j].Pricing = nil
			}
		}
		out = append(out, userAvailableChannel{Name: channel.Name, Platforms: sections})
	}
	response.Success(c, out)
}

// GetPublic never consults a user identity or model-plaza access switches. The
// homepage price list only contains the standard public-group catalog.
func (h *ModelPlazaHandler) GetPublic(c *gin.Context) {
	groups, err := h.plazaService.ListGroups(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	visible := filterPlazaVisibleGroups(groups, nil, false)
	out := make([]modelPlazaGroup, 0, len(visible))
	for i := range visible {
		out = append(out, toModelPlazaGroupDTO(&visible[i], nil))
	}
	response.Success(c, modelPlazaResponse{Groups: out})
}
