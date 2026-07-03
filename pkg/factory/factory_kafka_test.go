// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"context"
	"time"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	kafkamocks "github.com/bborbe/kafka/mocks"
	libtime "github.com/bborbe/time"
	timemocks "github.com/bborbe/time/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-pi/pkg/factory"
)

// This exercises the REAL factory.CreateKafkaResultDeliverer, which wraps the
// REAL delivery.NewKafkaResultDeliverer, fed a fake libkafka.SyncProducer, so a
// fat-fingered topicPrefix (e.g. always "") would be caught: the sender publishes
// a command, so the topic uses the CommandTopic ("request") suffix.
var _ = Describe("CreateKafkaResultDeliverer (topic prefix wiring)", func() {
	var (
		ctx             context.Context
		syncProducer    *kafkamocks.KafkaSyncProducer
		clock           *timemocks.CurrentDateTimeGetter
		taskID          agentlib.TaskIdentifier
		originalContent string
	)

	BeforeEach(func() {
		ctx = context.Background()
		syncProducer = &kafkamocks.KafkaSyncProducer{}
		syncProducer.SendMessageReturns(int32(0), int64(123), nil)
		clock = &timemocks.CurrentDateTimeGetter{}
		clock.NowReturns(libtime.DateTime(time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)))
		taskID = agentlib.TaskIdentifier("task-abc-123")
		originalContent = "---\ntitle: My Task\nstatus: in_progress\n---\n\nBody.\n"
	})

	It("publishes to the develop-prefixed request topic when topicPrefix is \"develop\"", func() {
		deliverer := factory.CreateKafkaResultDeliverer(
			syncProducer,
			base.TopicPrefix("develop"),
			taskID,
			originalContent,
			clock,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status: agentlib.AgentStatusDone,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(syncProducer.SendMessageCallCount()).To(Equal(1))
		_, msg := syncProducer.SendMessageArgsForCall(0)
		Expect(msg.Topic).To(Equal(libkafka.Topic("develop-agent-task-v1-request").String()))
	})

	It("publishes to the master-prefixed request topic when topicPrefix is \"master\"", func() {
		deliverer := factory.CreateKafkaResultDeliverer(
			syncProducer,
			base.TopicPrefix("master"),
			taskID,
			originalContent,
			clock,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status: agentlib.AgentStatusDone,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(syncProducer.SendMessageCallCount()).To(Equal(1))
		_, msg := syncProducer.SendMessageArgsForCall(0)
		Expect(msg.Topic).To(Equal(libkafka.Topic("master-agent-task-v1-request").String()))
	})

	It("publishes to the unprefixed request topic when topicPrefix is empty", func() {
		deliverer := factory.CreateKafkaResultDeliverer(
			syncProducer,
			base.TopicPrefix(""),
			taskID,
			originalContent,
			clock,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status: agentlib.AgentStatusDone,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(syncProducer.SendMessageCallCount()).To(Equal(1))
		_, msg := syncProducer.SendMessageArgsForCall(0)
		Expect(msg.Topic).To(Equal(libkafka.Topic("agent-task-v1-request").String()))
	})
})
