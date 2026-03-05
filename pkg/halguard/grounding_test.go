package halguard

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests are internal (package halguard) to test unexported functions.

var _ = Describe("Grounding", func() {
	Describe("scoreGoal", func() {
		Context("genuine goals", func() {
			It("should have zero penalty for a simple file search", func() {
				penalty, signals := scoreGoal("Find all Go files that import the fmt package")
				Expect(penalty).To(BeNumerically("<=", 0.1))
				Expect(signals.HasAny()).To(BeFalse())
			})

			It("should have zero penalty for a code change request", func() {
				penalty, signals := scoreGoal("Add error handling to the processOrder function in orders.go")
				Expect(penalty).To(BeNumerically("<=", 0.1))
				Expect(signals.HasAny()).To(BeFalse())
			})

			It("should have zero penalty for a deployment task", func() {
				penalty, signals := scoreGoal("Deploy the staging branch to the QA environment using terraform apply")
				Expect(penalty).To(BeNumerically("<=", 0.1))
				Expect(signals.HasAny()).To(BeFalse())
			})
		})

		Context("role-play goals", func() {
			It("should detect 'you are an SRE' pattern", func() {
				penalty, signals := scoreGoal("You are an SRE engineer responding to a production incident")
				Expect(penalty).To(BeNumerically(">", 0.3))
				Expect(signals.RolePlay).To(BeNumerically(">", 0))
			})

			It("should detect 'imagine you're' pattern", func() {
				penalty, signals := scoreGoal("Imagine you're a DevOps engineer handling a critical outage")
				Expect(penalty).To(BeNumerically(">", 0.3))
				Expect(signals.RolePlay).To(BeNumerically(">", 0))
			})

			It("should detect 'pretend to be' pattern", func() {
				penalty, signals := scoreGoal("Pretend to be an incident commander for this drill exercise")
				Expect(penalty).To(BeNumerically(">", 0.3))
				Expect(signals.RolePlay).To(BeNumerically(">", 0))
			})

			It("should detect simulate/roleplay keywords", func() {
				penalty, signals := scoreGoal("Simulate a database outage scenario")
				Expect(penalty).To(BeNumerically(">", 0.2))
				Expect(signals.RolePlay).To(BeNumerically(">", 0))
			})

			It("should detect 'act as' pattern", func() {
				penalty, signals := scoreGoal("Act as a software engineer and review this incident")
				Expect(penalty).To(BeNumerically(">", 0.2))
				Expect(signals.RolePlay).To(BeNumerically(">", 0))
			})
		})

		Context("fabrication pattern goals", func() {
			It("should detect specific latency spike claims", func() {
				penalty, signals := scoreGoal("The p99 latency spiked from 50ms to 2000ms")
				Expect(penalty).To(BeNumerically(">", 0.1))
				Expect(signals.FabricationPattern).To(BeNumerically(">", 0))
			})

			It("should detect error rate jump claims", func() {
				penalty, signals := scoreGoal("Our error rate jumped from 0.01% to 15% suddenly")
				Expect(penalty).To(BeNumerically(">", 0.1))
				Expect(signals.FabricationPattern).To(BeNumerically(">", 0))
			})

			It("should detect CPU usage spike claims", func() {
				penalty, signals := scoreGoal("CPU usage spiked to 98% on the payment service")
				Expect(penalty).To(BeNumerically(">", 0.1))
				Expect(signals.FabricationPattern).To(BeNumerically(">", 0))
			})

			It("should detect temporal references", func() {
				penalty, signals := scoreGoal("Check what happened in the last 30 minutes to our cluster")
				Expect(penalty).To(BeNumerically(">", 0.1))
				Expect(signals.FabricationPattern).To(BeNumerically(">", 0))
			})

			It("should detect 'users are reporting' pattern", func() {
				penalty, signals := scoreGoal("Users are reporting intermittent 500 errors on the checkout page")
				Expect(penalty).To(BeNumerically(">", 0.1))
				Expect(signals.FabricationPattern).To(BeNumerically(">", 0))
			})
		})

		Context("combined signals", func() {
			It("should have high penalty when multiple signals fire", func() {
				goal := "You are an SRE. The p99 latency increased from 100ms to 5000ms. " +
					"CPU usage spiked to 95%. The service has been down for 45 minutes. " +
					"Users are reporting 500 errors."
				penalty, signals := scoreGoal(goal)
				Expect(penalty).To(BeNumerically(">", 0.6))
				Expect(signals.RolePlay).To(BeNumerically(">", 0))
				Expect(signals.FabricationPattern).To(BeNumerically(">", 0))
			})

			It("should cap penalty at 1.0", func() {
				goal := "You are an SRE responding to a production incident. " +
					"Imagine you're the on-call engineer. Simulate a tabletop exercise. " +
					"P99 latency spiked from 10ms to 10000ms. Error rate jumped from 0% to 50%. " +
					"CPU usage spiked to 100%. Memory usage increased to 99%. " +
					"The service has been down for 2 hours. Users are reporting errors. " +
					"The dashboard shows critical alerts."
				penalty, _ := scoreGoal(goal)
				Expect(penalty).To(BeNumerically("<=", 1.0))
			})
		})

		Context("second person role assignment", func() {
			It("should detect 'You are' at start of goal", func() {
				_, signals := scoreGoal("You are responsible for fixing the deployment pipeline")
				Expect(signals.SecondPersonRole).To(BeNumerically(">", 0))
			})

			It("should detect 'You're' at start of goal", func() {
				_, signals := scoreGoal("You're an incident commander for this drill")
				Expect(signals.SecondPersonRole).To(BeNumerically(">", 0))
			})

			It("should NOT trigger for mid-sentence 'you are'", func() {
				_, signals := scoreGoal("The error means you are missing a dependency")
				Expect(signals.SecondPersonRole).To(Equal(0.0))
			})
		})

		Context("specific metrics", func() {
			It("should detect multiple specific metrics", func() {
				_, signals := scoreGoal("Latency is 342ms and error rate is 2.4% with 1500 requests/s")
				Expect(signals.SpecificMetrics).To(BeNumerically(">", 0))
			})

			It("should NOT trigger for a single metric", func() {
				_, signals := scoreGoal("The timeout is 30 seconds")
				Expect(signals.SpecificMetrics).To(Equal(0.0))
			})
		})

		Context("temporal urgency", func() {
			It("should detect 'production is down'", func() {
				_, signals := scoreGoal("Production is down and we need help immediately")
				Expect(signals.TemporalUrgency).To(BeNumerically(">", 0))
			})

			It("should detect 'urgent' and 'critical'", func() {
				_, signals := scoreGoal("This is an urgent critical issue that needs attention right now")
				Expect(signals.TemporalUrgency).To(BeNumerically(">", 0))
			})
		})
	})

	Describe("scoreRegexMatches", func() {
		It("should return 0 for no matches", func() {
			score := scoreRegexMatches("hello world", rolePlayPatterns)
			Expect(score).To(Equal(0.0))
		})

		It("should return 0.7 for a single match", func() {
			score := scoreRegexMatches("You are an SRE engineer", rolePlayPatterns)
			Expect(score).To(BeNumerically("~", 0.7, 0.01))
		})

		It("should return higher score for multiple matches", func() {
			score := scoreRegexMatches("You are an SRE. Simulate a production incident. Imagine you're on-call.", rolePlayPatterns)
			Expect(score).To(BeNumerically(">", 0.7))
		})

		It("should cap at 1.0", func() {
			// All patterns matching
			text := "You are an SRE. Simulate a tabletop exercise. Imagine you're responding. Pretend to be on-call. In this scenario, handle the incident. Mock incident drill."
			score := scoreRegexMatches(text, rolePlayPatterns)
			Expect(score).To(BeNumerically("<=", 1.0))
		})
	})

	Describe("informationDensity", func() {
		It("should return 0 for short text", func() {
			d := informationDensity("hi")
			Expect(d).To(Equal(0.0))
		})

		It("should return low density for generic text", func() {
			d := informationDensity("This is a simple sentence about nothing in particular that should have low density")
			Expect(d).To(BeNumerically("<", 0.3))
		})

		It("should return higher density for technical text with numbers", func() {
			d := informationDensity("The service has latency 500ms throughput 1000 error rate 5 timeout 30 cpu 95 memory 90 disk 80")
			Expect(d).To(BeNumerically(">", 0.3))
		})
	})

	Describe("countSentences", func() {
		It("should return 1 for no sentence endings", func() {
			c := countSentences("hello world")
			Expect(c).To(Equal(1))
		})

		It("should count periods", func() {
			c := countSentences("First sentence. Second sentence. Third.")
			Expect(c).To(Equal(3))
		})

		It("should count question marks", func() {
			c := countSentences("Is this a question? Yes it is.")
			Expect(c).To(Equal(2))
		})
	})

	Describe("segmentIntoBlocks", func() {
		It("should split by paragraph", func() {
			text := "First paragraph.\n\nSecond paragraph."
			blocks := segmentIntoBlocks(text)
			Expect(blocks).To(HaveLen(2))
			Expect(blocks[0]).To(Equal("First paragraph."))
			Expect(blocks[1]).To(Equal("Second paragraph."))
		})

		It("should keep headers as separate blocks", func() {
			text := "# Header\nSome content.\n## Sub Header\nMore content."
			blocks := segmentIntoBlocks(text)
			Expect(blocks).To(ContainElement("# Header"))
			Expect(blocks).To(ContainElement("## Sub Header"))
		})

		It("should keep list items as separate blocks", func() {
			text := "- Item 1\n- Item 2\n* Item 3"
			blocks := segmentIntoBlocks(text)
			Expect(blocks).To(HaveLen(3))
		})

		It("should handle empty text", func() {
			blocks := segmentIntoBlocks("")
			Expect(blocks).To(BeEmpty())
		})

		It("should join continued lines until sentence end", func() {
			text := "This is a long sentence that\nspans multiple lines. And this is another."
			blocks := segmentIntoBlocks(text)
			Expect(blocks).To(HaveLen(1))
			Expect(blocks[0]).To(ContainSubstring("spans multiple lines"))
		})
	})

	Describe("endsWithSentence", func() {
		It("should detect period", func() {
			Expect(endsWithSentence("Hello.")).To(BeTrue())
		})

		It("should detect question mark", func() {
			Expect(endsWithSentence("Hello?")).To(BeTrue())
		})

		It("should detect exclamation mark", func() {
			Expect(endsWithSentence("Hello!")).To(BeTrue())
		})

		It("should handle trailing quotes", func() {
			Expect(endsWithSentence(`He said "hello."`)).To(BeTrue())
		})

		It("should return false for no ending", func() {
			Expect(endsWithSentence("Hello")).To(BeFalse())
		})

		It("should return false for empty string", func() {
			Expect(endsWithSentence("")).To(BeFalse())
		})
	})

	Describe("isSpecificTerm", func() {
		It("should detect numbers", func() {
			Expect(isSpecificTerm("123")).To(BeTrue())
		})

		It("should detect technical terms", func() {
			Expect(isSpecificTerm("latency")).To(BeTrue())
			Expect(isSpecificTerm("cpu")).To(BeTrue())
			Expect(isSpecificTerm("database")).To(BeTrue())
		})

		It("should detect term variations via prefix matching", func() {
			Expect(isSpecificTerm("latencies")).To(BeTrue())
			Expect(isSpecificTerm("degraded")).To(BeTrue())
			Expect(isSpecificTerm("failures")).To(BeTrue())
			Expect(isSpecificTerm("crashed")).To(BeTrue())
			Expect(isSpecificTerm("leaking")).To(BeTrue())
			Expect(isSpecificTerm("restarted")).To(BeTrue())
			Expect(isSpecificTerm("monitoring")).To(BeTrue())
		})

		It("should detect version strings", func() {
			Expect(isSpecificTerm("v2")).To(BeTrue())
			Expect(isSpecificTerm("V1")).To(BeTrue())
		})

		It("should not match generic words", func() {
			Expect(isSpecificTerm("hello")).To(BeFalse())
			Expect(isSpecificTerm("world")).To(BeFalse())
			Expect(isSpecificTerm("please")).To(BeFalse())
		})
	})

	Describe("narrativeArcScore", func() {
		It("should return 0 for a simple task request", func() {
			score := narrativeArcScore("Find all Go files that import fmt")
			Expect(score).To(Equal(0.0))
		})

		It("should return 0 for a generic question", func() {
			score := narrativeArcScore("What is the best way to handle errors in Go?")
			Expect(score).To(Equal(0.0))
		})

		It("should detect situation+cause+action (full narrative arc)", func() {
			score := narrativeArcScore(
				"Our API has been experiencing slowdowns due to a connection pool leak. " +
					"Investigate the database connections and fix the pooling configuration.",
			)
			Expect(score).To(BeNumerically(">", 0.8))
		})

		It("should detect situation+action (partial arc)", func() {
			score := narrativeArcScore(
				"The billing service seems to be failing intermittently. " +
					"Check the logs and find out why.",
			)
			Expect(score).To(BeNumerically(">", 0.5))
		})

		It("should detect situation+cause without action", func() {
			score := narrativeArcScore(
				"The deployment started to fail after the config change.",
			)
			Expect(score).To(BeNumerically(">", 0.5))
		})

		It("should cap at 1.0", func() {
			score := narrativeArcScore(
				"The service has been down since the deployment. " +
					"Root cause was a memory leak introduced in the latest release. " +
					"Immediately investigate and fix the issue.",
			)
			Expect(score).To(BeNumerically("<=", 1.0))
		})
	})

	Describe("regression: creative rephrasing bypasses regex", func() {
		It("should still catch fabricated scenarios using naturalistic phrasing", func() {
			// This goal avoids the specific fabrication regex patterns
			// (no "p99 latency spiked from X to Y", no "error rate jumped")
			// but still describes a fabricated incident scenario.
			goal := "Our payment processing has been experiencing severe degradation " +
				"ever since the recent release went out. The response times went from " +
				"acceptable to unusable. This was caused by a memory leak in the " +
				"transaction handler. Immediately check the Grafana dashboards and " +
				"generate an incident report."
			penalty, signals := scoreGoal(goal)
			// Should catch via narrative arc + information density + temporal urgency
			Expect(penalty).To(BeNumerically(">", 0.1),
				"naturalistic rephrasing should still trigger some penalty via structural signals")
			// At least one of the structural signals should fire
			hasStructuralSignal := signals.InformationDensity > 0 || signals.TemporalUrgency > 0
			Expect(hasStructuralSignal).To(BeTrue(),
				"expected information density or temporal urgency to fire even without regex matches")
		})
	})
})

var _ = Describe("Helpers", func() {
	Describe("extractJSON", func() {
		It("should extract JSON object from plain text", func() {
			result := extractJSON(`{"key": "value"}`)
			Expect(result).To(Equal(`{"key": "value"}`))
		})

		It("should extract JSON array from plain text", func() {
			result := extractJSON(`[{"item": 1}]`)
			Expect(result).To(Equal(`[{"item": 1}]`))
		})

		It("should strip markdown code fences", func() {
			result := extractJSON("```json\n{\"key\": \"value\"}\n```")
			Expect(result).To(Equal(`{"key": "value"}`))
		})

		It("should extract JSON from preamble text", func() {
			result := extractJSON("Here is the result: {\"key\": \"value\"}")
			Expect(result).To(Equal(`{"key": "value"}`))
		})

		It("should handle text with no JSON", func() {
			result := extractJSON("no json here")
			Expect(result).To(Equal("no json here"))
		})

		It("should handle nested JSON", func() {
			result := extractJSON(`{"outer": {"inner": true}}`)
			Expect(result).To(Equal(`{"outer": {"inner": true}}`))
		})
	})

	Describe("labelFromBool", func() {
		It("should return BlockAccurate for true", func() {
			Expect(labelFromBool(true)).To(Equal(BlockAccurate))
		})

		It("should return BlockContradiction for false", func() {
			Expect(labelFromBool(false)).To(Equal(BlockContradiction))
		})
	})

	Describe("toBlockLabel", func() {
		It("should parse ACCURATE", func() {
			Expect(toBlockLabel("ACCURATE")).To(Equal(BlockAccurate))
		})

		It("should parse CONTRADICTION", func() {
			Expect(toBlockLabel("CONTRADICTION")).To(Equal(BlockContradiction))
		})

		It("should parse case insensitively", func() {
			Expect(toBlockLabel("accurate")).To(Equal(BlockAccurate))
			Expect(toBlockLabel("Contradiction")).To(Equal(BlockContradiction))
		})

		It("should default to NEUTRAL for unknown labels", func() {
			Expect(toBlockLabel("UNKNOWN")).To(Equal(BlockNeutral))
			Expect(toBlockLabel("")).To(Equal(BlockNeutral))
		})
	})

	Describe("countContradictions", func() {
		It("should count zero for no contradictions", func() {
			scores := BlockScores{
				{Label: BlockAccurate},
				{Label: BlockNeutral},
			}
			Expect(scores.countContradictions()).To(Equal(0))
		})

		It("should count contradictions correctly", func() {
			scores := BlockScores{
				{Label: BlockAccurate},
				{Label: BlockContradiction},
				{Label: BlockNeutral},
				{Label: BlockContradiction},
			}
			Expect(scores.countContradictions()).To(Equal(2))
		})
	})

	Describe("modelKeys", func() {
		It("should extract keys", func() {
			models := []verificationModel{
				{key: "openai/gpt-4", model: nil},
				{key: "anthropic/claude", model: nil},
			}
			keys := modelKeys(models)
			Expect(keys).To(Equal([]string{"openai/gpt-4", "anthropic/claude"}))
		})
	})
})

var _ = Describe("Config", func() {
	Describe("defaults", func() {
		It("should apply default values to zero Config", func() {
			cfg := Config{}.defaults()
			Expect(cfg.LightThresholdChars).To(Equal(200))
			Expect(cfg.FullThresholdChars).To(Equal(500))
			Expect(cfg.CrossModelSamples).To(Equal(3))
			Expect(cfg.MaxBlocksToJudge).To(Equal(20))
			Expect(cfg.PreCheckThreshold).To(BeNumerically("~", 0.4, 0.01))
		})

		It("should not override non-zero values", func() {
			cfg := Config{
				LightThresholdChars: 100,
				FullThresholdChars:  300,
				CrossModelSamples:   5,
				MaxBlocksToJudge:    10,
				PreCheckThreshold:   0.6,
			}.defaults()
			Expect(cfg.LightThresholdChars).To(Equal(100))
			Expect(cfg.FullThresholdChars).To(Equal(300))
			Expect(cfg.CrossModelSamples).To(Equal(5))
			Expect(cfg.MaxBlocksToJudge).To(Equal(10))
			Expect(cfg.PreCheckThreshold).To(BeNumerically("~", 0.6, 0.01))
		})
	})
})
