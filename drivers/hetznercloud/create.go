// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package hetznercloud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/drone/autoscaler"
	"github.com/drone/autoscaler/logger"

	"github.com/hetznercloud/hcloud-go/hcloud"
)

func (p *provider) Create(ctx context.Context, opts autoscaler.InstanceCreateOpts) (*autoscaler.Instance, error) {
	p.init.Do(func() {
		p.setup(ctx)
	})

	buf := new(bytes.Buffer)
	err := p.userdata.Execute(buf, &opts)
	if err != nil {
		return nil, err
	}

	req := hcloud.ServerCreateOpts{
		Name:     opts.Name,
		UserData: buf.String(),
		ServerType: &hcloud.ServerType{
			Name: p.serverType,
		},
		Image: &hcloud.Image{
			Name: p.image,
		},
		SSHKeys: []*hcloud.SSHKey{
			{
				ID: p.key,
			},
		},
	}

	if p.network != "" {
		net, _, err := p.client.Network.GetByName(ctx, p.network)
		if err != nil {
			return nil, err
		} else if net == nil {
			return nil, errors.New(fmt.Sprintf("Network %s not found.", p.network))
		}
		req.Networks = append(req.Networks, net)
	}

	var privNet *hcloud.Network
	if p.private != "" {
		privNet, _, err = p.client.Network.GetByName(ctx, p.private)
		if err != nil {
			return nil, err
		} else if privNet == nil {
			return nil, errors.New(fmt.Sprintf("Network %s not found.", p.private))
		}
		req.Networks = append(req.Networks, privNet)
	}

	datacenter := "unknown"

	if p.datacenter != "" {
		req.Datacenter = &hcloud.Datacenter{
			Name: p.datacenter,
		}

		datacenter = p.datacenter
	}

	logger := logger.FromContext(ctx).
		WithField("datacenter", datacenter).
		WithField("image", req.Image.Name).
		WithField("serverType", req.ServerType.Name).
		WithField("name", req.Name)

	logger.Debugln("instance create")

	resp, _, err := p.client.Server.Create(ctx, req)
	if err != nil {
		logger.WithError(err).
			Errorln("cannot create instance")
		return nil, err
	}

	logger.
		WithField("name", req.Name).
		WithField("full", req).
		Infoln("instance created")

	var ip string
	if p.private != "" {
		_, errC := p.client.Action.WatchOverallProgress(ctx, resp.NextActions)
		if err := <-errC; err != nil {
			return nil, err
		}
		s, _, err := p.client.Server.GetByID(context.Background(), resp.Server.ID)
		if err != nil {
			return nil, err
		}
		for _, net := range s.PrivateNet {
			if net.Network.ID == privNet.ID {
				ip = net.IP.String()
				break
			}
		}
	} else {
		ip = resp.Server.PublicNet.IPv4.IP.String()
	}
	if ip == "" {
		return nil, errors.New("Instance address not set (Private network not found on instance?).")
	}

	return &autoscaler.Instance{
		Provider: autoscaler.ProviderHetznerCloud,
		ID:       strconv.Itoa(resp.Server.ID),
		Name:     resp.Server.Name,
		Address:  ip,
		Size:     req.ServerType.Name,
		Region:   datacenter,
		Image:    req.Image.Name,
	}, nil
}
