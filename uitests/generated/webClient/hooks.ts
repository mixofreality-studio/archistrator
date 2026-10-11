// Hooks for webClient scenario tests. Generated ONCE by testgen; owned by the construction agent.
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import * as fs from 'node:fs';
import * as http from 'node:http';
import * as net from 'node:net';
import * as os from 'node:os';
import * as path from 'node:path';
import { expect, test, type Page } from '@playwright/test';

// Hooked steps (scenario / seq → binding JSON):
//   add-a-use-case-to-an-in-flight-project-P1 / 1: {"seq":1,"operation":"AmendArtifact","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-coreUseCasesReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-amend-open\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"amend-rationale\",\"value\":\"Add the use case 'Export the project audit trail' (variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"amend-submit\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"generating-amending-notice\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   add-a-use-case-to-an-in-flight-project-P1 / 2: {"seq":2,"operation":"DecideReview","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-coreUseCasesReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"submit-bar-primary\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-amend-open\"},{\"visible\":\"artifact-render\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   add-a-use-case-to-an-in-flight-project-P2 / 1: {"seq":1,"operation":"AmendArtifact","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-coreUseCasesReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-amend-open\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"amend-rationale\",\"value\":\"Add the use case 'Export the project audit trail' (variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"amend-submit\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"generating-amending-notice\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   add-a-use-case-to-an-in-flight-project-P2 / 2: {"seq":2,"operation":"DecideReview","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-coreUseCasesReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"submit-bar-primary\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-amend-open\"},{\"visible\":\"artifact-render\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   add-a-use-case-to-an-in-flight-project-P2 / 3: {"seq":3,"operation":"MarkReviewedUnaffected","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/architecture\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-architectureReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"stale-mark-reviewed\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"stale-ack-note\",\"value\":\"reviewed — unaffected\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"stale-ack-confirm\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"artifact-render\"},{\"visible\":\"activity-amend-open\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   add-a-use-case-to-an-in-flight-project-P3 / 1: {"seq":1,"operation":"AmendArtifact","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-coreUseCasesReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-amend-open\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"amend-rationale\",\"value\":\"Add the use case 'Export the project audit trail' (variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"amend-submit\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"generating-amending-notice\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   add-a-use-case-to-an-in-flight-project-P3 / 2: {"seq":2,"operation":"DecideReview","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-coreUseCasesReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"submit-bar-primary\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-amend-open\"},{\"visible\":\"artifact-render\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   add-a-use-case-to-an-in-flight-project-P4 / 1: {"seq":1,"operation":"AmendArtifact","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-coreUseCasesReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-amend-open\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"amend-rationale\",\"value\":\"Add the use case 'Export the project audit trail' (variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"amend-submit\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"generating-amending-notice\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   add-a-use-case-to-an-in-flight-project-P4 / 2: {"seq":2,"operation":"DecideReview","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-coreUseCasesReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"submit-bar-primary\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-amend-open\"},{\"visible\":\"artifact-render\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   add-a-use-case-to-an-in-flight-project-P4 / 3: {"seq":3,"operation":"MarkReviewedUnaffected","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/architecture\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-architectureReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"stale-mark-reviewed\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"stale-ack-note\",\"value\":\"reviewed — unaffected\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"stale-ack-confirm\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"artifact-render\"},{\"visible\":\"activity-amend-open\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   ask-a-clarifying-question-during-review-P1 / 1: {"seq":1,"operation":"AskReviewQuestions","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-missionReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"comment-list-item-button-obj-1-0\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-type-question\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-addressee-pm\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"chat-input\",\"value\":\"Why is objective 1 measured per project rather than per activity?\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-send\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"submit-bar-primary\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"comment-margin\"},{\"visible\":\"artifact-render\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   ask-a-clarifying-question-during-review-P2 / 1: {"seq":1,"operation":"AskReviewQuestions","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-missionReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"comment-list-item-button-obj-1-0\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-type-question\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-addressee-pm\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"chat-input\",\"value\":\"Why is objective 1 measured per project rather than per activity?\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-send\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"submit-bar-primary\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"comment-margin\"},{\"visible\":\"artifact-render\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   ask-a-clarifying-question-during-review-P3 / 1: {"seq":1,"operation":"AskReviewQuestions","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-missionReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"comment-list-item-button-obj-1-0\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-type-question\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-addressee-pm\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"chat-input\",\"value\":\"Why is objective 1 measured per project rather than per activity?\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-send\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"submit-bar-primary\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"comment-margin\"},{\"visible\":\"artifact-render\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   ask-a-clarifying-question-during-review-P4 / 1: {"seq":1,"operation":"AskReviewQuestions","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-missionReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"comment-list-item-button-obj-1-0\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-type-question\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-addressee-pm\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"chat-input\",\"value\":\"Why is objective 1 measured per project rather than per activity?\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"chat-send\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"submit-bar-primary\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"comment-margin\"},{\"visible\":\"artifact-render\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P1 / 1: {"seq":1,"operation":"OpenActivity","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/plan\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"plan-row-C-review-engine\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-construction\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/C-review-engine\"},{\"visible\":\"activity-screen\"},{\"visible\":\"activity-dispatch-body\"},{\"visible\":\"activity-task-facts\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P2 / 1: {"seq":1,"operation":"OpenActivity","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/plan\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"plan-row-C-review-engine\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-construction\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/C-review-engine\"},{\"visible\":\"activity-dispatch-body\"},{\"visible\":\"activity-override-bar\"},{\"visible\":\"activity-steer\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P2 / 2: {"seq":2,"operation":"SteerActivity","inputs":[{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"activity-override-note\",\"value\":\"Retry: the last attempt failed on a flaky CI runner, not on the code; rerun with the change unchanged.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-steer\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-sub-attempts\"},{\"visible\":\"activity-dispatch-body\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P3 / 1: {"seq":1,"operation":"OpenActivity","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/plan\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"plan-row-C-review-engine\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-codeReview\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/C-review-engine\"},{\"visible\":\"activity-review-body\"},{\"visible\":\"activity-reviewers-strip\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P4 / 1: {"seq":1,"operation":"OpenActivity","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/plan\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"plan-row-C-review-engine\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-codeReview\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/C-review-engine\"},{\"visible\":\"activity-review-body\"},{\"visible\":\"activity-reviewers-strip\"},{\"visible\":\"submit-bar-primary\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P4 / 2: {"seq":2,"operation":"DecideReview","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"submit-bar-primary\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-review-body\"},{\"visible\":\"activity-reviewers-strip\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P5 / 1: {"seq":1,"operation":"OpenActivity","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/plan\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"plan-row-C-review-engine\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-construction\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/C-review-engine\"},{\"visible\":\"activity-dispatch-body\"},{\"visible\":\"activity-sub-attempts\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P6 / 1: {"seq":1,"operation":"OpenActivity","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/plan\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"plan-row-C-review-engine\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-construction\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/C-review-engine\"},{\"visible\":\"activity-dispatch-body\"},{\"visible\":\"activity-override-bar\"},{\"visible\":\"activity-steer\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P6 / 2: {"seq":2,"operation":"SteerActivity","inputs":[{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"activity-override-note\",\"value\":\"Retry: the last attempt failed on a flaky CI runner, not on the code; rerun with the change unchanged.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-steer\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-sub-attempts\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P6 / 3: {"seq":3,"operation":"SteerActivity","inputs":[{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"activity-override-note\",\"value\":\"Retry again: pin the integration test's Temporal namespace; the earlier failure was a namespace collision.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-steer\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-sub-attempts\"},{\"visible\":\"activity-dispatch-body\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   execute-a-project-activity-P7 / 1: {"seq":1,"operation":"OpenActivity","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/plan\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"plan-row-C-review-engine\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-construction\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/C-review-engine\"},{\"visible\":\"activity-dispatch-body\"},{\"visible\":\"lifecycle-revision-select\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   replan-under-scope-change-P1 / 1: {"seq":1,"operation":"AmendArtifact","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/projectDesign\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-sdpReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-amend-architecture\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-architectureReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-amend-open\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"amend-rationale\",\"value\":\"Architecture change: split billing into metering and invoicing components; this is not a scope change.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"amend-submit\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/architecture\"},{\"visible\":\"generating-amending-notice\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   replan-under-scope-change-P2 / 1: {"seq":1,"operation":"AmendArtifact","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/projectDesign\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-sdpReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-amend-open\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"amend-rationale\",\"value\":\"Scope change: add the audit-export use case; re-plan from the activity list against the current committed network.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"amend-submit\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/projectDesign\"},{\"visible\":\"generating-amending-notice\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   replan-under-scope-change-P3 / 1: {"seq":1,"operation":"AmendArtifact","inputs":[{"name":"action","value":"{\"kind\":\"navigate\",\"value\":\"/project/dct-owner%2Fdct-delivery/activity/projectDesign\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-sdpReview\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"activity-amend-open\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"amend-rationale\",\"value\":\"Scope change: add the audit-export use case; re-plan from the activity list against the current committed network.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"amend-submit\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"url\":\"/project/dct-owner%2Fdct-delivery/activity/projectDesign\"},{\"visible\":\"generating-amending-notice\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   send-back-change-requests-for-a-redraft-P1 / 2: {"seq":2,"operation":"ReviewRedraft","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-missionReview\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"artifact-render\"},{\"visible\":\"comment-margin\"},{\"visible\":\"submit-bar-primary\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   send-back-change-requests-for-a-redraft-P2 / 2: {"seq":2,"operation":"ReviewRedraft","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-missionReview\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"artifact-render\"},{\"visible\":\"comment-margin\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
//   send-back-change-requests-for-a-redraft-P2 / 4: {"seq":4,"operation":"ReviewRedraft","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"lifecycle-node-missionReview\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"artifact-render\"},{\"visible\":\"comment-margin\"},{\"visible\":\"submit-bar-primary\"}]","errorExpected":false},"probes":null,"unobservable":null,"hook":true}

// ── What every scenario runs against ─────────────────────────────────────────
//
// THE STACK is the real one, and these hooks own it (the generated config has no
// webServer block: hooks.before brings up whatever a scenario needs). Every scenario
// gets a stack of its own, so nothing one scenario did is read by the next:
//
//   - a fresh Temporal dev server (in memory): no workflow of an earlier scenario
//     exists to answer this one's queries;
//   - a fresh project repo: the LOCAL profile's state repo, an empty `main`;
//   - the Go server (cmd/server) on the LOCAL profile over both, at the port the SPA's
//     dev server proxies /api to (UITESTS_API_PORT; the SPA is
//     PLAYWRIGHT_TEST_BASE_URL and stays up across scenarios), with construction
//     REAL (ARCHISTRATOR_CONSTRUCTION_DRYRUN=false): every agent job runs through the
//     local executor (a git worktree per job, the prompt surface seated by the state
//     tool cmd/aiarch-state-mcp, `claude` started in it, the run judged by whether
//     its branch advanced);
//   - Postgres is the harness's (UITESTS_POSTGRES_URL), shared.
//
// THE ONE DOUBLE is the coding agent: the `claude` the venue starts is the scripted
// agent (the venue section below), first on the server's PATH, so the real Claude
// Code CLI is never reached. It hands its job to this process, which does in the
// job's worktree what a real agent does there: a design job drives the REAL state
// tool over MCP (putDraftModel / setCritiqueVerdict / respondToReviewComment, then
// publishDraft); a construction job commits its work, and its detailed design records
// the component's contract (recordServiceContract). It ends as the scenario scripts
// it: succeeds, fails, or is still running when the operator looks.
//
// ARRANGEMENT goes through actors and public operations only (rulings r2/r4): the
// customer starts the project (StartProject, with the research the app's own design
// state was drafted from); the agent drafts each design slot, and what it authors is
// the slot as the app's own committed design state holds it, READ as source data; the
// human decides each gate (SubmitReviewDecision, the op the SPA's submit bar calls).
// No project state, workflow state, attempt, verdict or episode is written by a hook.
// A hook arranges and settles; the generated spec asserts.

const PROJECT_ID = 'dct-owner/dct-delivery';
const PROJECT_OWNER = 'dct-owner';
const PROJECT_PATH = encodeURIComponent(PROJECT_ID);

const REPO_ROOT = path.resolve(import.meta.dirname, '../../..');
/** The app's own committed design state: source data for what the scripted agent authors. */
const DESIGN_STATE = path.join(REPO_ROOT, '.aiarch/state/project.json');

const API_PORT = Number(process.env['UITESTS_API_PORT'] ?? '8888');
const POSTGRES_URL =
  process.env['UITESTS_POSTGRES_URL'] ??
  'postgres://archistrator:archistrator@localhost:5432/archistrator?sslmode=disable';
/** Where each scenario's stack keeps its repo, logs and venue. */
const WORK_ROOT = process.env['UITESTS_WORK_DIR'] ?? path.join(os.tmpdir(), 'uitests-webclient');

/** How long a walk to a scenario's starting state may take on top of the scenario's own budget. */
const ARRANGE_BUDGET_MS = 20 * 60_000;
/** How long one wait on the asynchronous walk may take. */
const WALK_WINDOW_MS = 6 * 60_000;

// ── The stack ────────────────────────────────────────────────────────────────

type Stack = {
  dir: string;
  repo: string;
  temporal: ChildProcess;
  server: ChildProcess;
};

let stack: Stack | undefined;
const children = new Set<ChildProcess>();

process.on('exit', () => {
  for (const c of children) c.kill('SIGKILL');
});

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      srv.close(() => {
        if (addr !== null && typeof addr === 'object') resolve(addr.port);
        else reject(new Error('uitests: no free port'));
      });
    });
  });
}

function run(cmd: string, args: string[], cwd: string, env?: NodeJS.ProcessEnv): string {
  return execFileSync(cmd, args, {
    cwd,
    env: env ?? process.env,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}

function git(cwd: string, ...args: string[]): string {
  return run(
    'git',
    ['-c', 'user.name=uitests', '-c', 'user.email=uitests@aiarch.local', ...args],
    cwd
  );
}

/** A binary the stack runs: the harness's prebuilt one, or built once from this checkout. */
const built = new Map<string, string>();
function binary(envName: string, pkg: string): string {
  const fromEnv = process.env[envName];
  if (fromEnv !== undefined && fromEnv !== '') return fromEnv;
  const cached = built.get(pkg);
  if (cached !== undefined) return cached;
  const out = path.join(WORK_ROOT, 'bin', path.basename(pkg));
  fs.mkdirSync(path.dirname(out), { recursive: true });
  run('go', ['build', '-o', out, pkg], path.join(REPO_ROOT, 'server'), {
    ...process.env,
    GOWORK: 'off',
  });
  built.set(pkg, out);
  return out;
}

function spawnLogged(
  cmd: string,
  args: string[],
  logFile: string,
  opts: { cwd: string; env: NodeJS.ProcessEnv }
): ChildProcess {
  const log = fs.openSync(logFile, 'a');
  const child = spawn(cmd, args, { cwd: opts.cwd, env: opts.env, stdio: ['ignore', log, log] });
  fs.closeSync(log);
  children.add(child);
  child.once('exit', () => children.delete(child));
  return child;
}

async function stopChild(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()));
  child.kill('SIGTERM');
  const timer = setTimeout(() => child.kill('SIGKILL'), 10_000);
  await exited;
  clearTimeout(timer);
}

async function waitFor(
  what: string,
  windowMs: number,
  probe: () => Promise<boolean>
): Promise<void> {
  const deadline = Date.now() + windowMs;
  while (Date.now() < deadline) {
    if (await probe()) return;
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`uitests: ${what} did not happen within ${String(windowMs / 1000)}s`);
}

function httpOK(url: string): Promise<boolean> {
  return new Promise((resolve) => {
    const req = http.get(url, (res) => {
      res.resume();
      resolve(res.statusCode !== undefined && res.statusCode < 300);
    });
    req.on('error', () => resolve(false));
    req.setTimeout(2_000, () => req.destroy());
  });
}

function tcpOpen(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const sock = net.connect(port, '127.0.0.1');
    sock.once('connect', () => {
      sock.destroy();
      resolve(true);
    });
    sock.once('error', () => resolve(false));
  });
}

/**
 * The project repo the LOCAL profile keeps the project in: a non-bare repository
 * (the venue requires it: its episode traces sit beside the git dir, never in it)
 * holding one empty commit on `main`, whose own checkout stays on a branch of its
 * own, so every project branch (main included) is only ever pushed to.
 */
function newProjectRepo(dir: string): string {
  const repo = path.join(dir, 'dct-delivery');
  fs.mkdirSync(repo, { recursive: true });
  git(repo, 'init', '-q', '-b', 'checkout');
  git(repo, 'commit', '-q', '--allow-empty', '-m', 'seed');
  git(repo, 'branch', 'main');
  return repo;
}

async function stopStack(): Promise<void> {
  const s = stack;
  stack = undefined;
  if (s === undefined) return;
  await stopChild(s.server);
  await stopChild(s.temporal);
}

/**
 * Bring up this scenario's stack: a fresh Temporal, a fresh repo, the server over both,
 * configured as the operator configured it (`config`: server settings on top of the
 * LOCAL profile's).
 */
async function bootStack(id: string, config: Record<string, string>): Promise<Stack> {
  await stopStack();
  const serverBin = binary('UITESTS_SERVER_BIN', './cmd/server');
  const stateTool = binary('UITESTS_STATE_MCP_BIN', './cmd/aiarch-state-mcp');
  const dir = path.join(WORK_ROOT, `${id}-${String(Date.now())}`);
  fs.mkdirSync(dir, { recursive: true });
  const repo = newProjectRepo(dir);
  const venueDir = await openVenue(dir);

  const [tPort, tHttp, tMetrics] = [await freePort(), await freePort(), await freePort()];
  const temporal = spawnLogged(
    'temporal',
    [
      'server',
      'start-dev',
      '--headless',
      '--ip',
      '127.0.0.1',
      '--port',
      String(tPort),
      '--http-port',
      String(tHttp),
      '--metrics-port',
      String(tMetrics),
      '--namespace',
      'aiarch-test',
      '--log-level',
      'error',
    ],
    path.join(dir, 'temporal.log'),
    { cwd: dir, env: process.env }
  );
  await waitFor('Temporal accepting connections', 60_000, () => tcpOpen(tPort));

  if (await tcpOpen(API_PORT)) {
    throw new Error(`uitests: port ${String(API_PORT)} is taken; the scenario's server needs it`);
  }
  const server = spawnLogged(serverBin, [], path.join(dir, 'server.log'), {
    cwd: dir,
    env: {
      PATH: `${venueDir}${path.delimiter}${process.env['PATH'] ?? ''}`,
      HOME: process.env['HOME'] ?? dir,
      USER: process.env['USER'] ?? 'uitests',
      TMPDIR: process.env['TMPDIR'] ?? os.tmpdir(),
      ARCHISTRATOR_LISTEN_ADDR: `:${String(API_PORT)}`,
      ARCHISTRATOR_POSTGRES_URL: POSTGRES_URL,
      ARCHISTRATOR_TEMPORAL_HOSTPORT: `127.0.0.1:${String(tPort)}`,
      ARCHISTRATOR_TEMPORAL_NAMESPACE: 'aiarch-test',
      ARCHISTRATOR_AUTH_DEV_MODE: 'true',
      ARCHISTRATOR_DEV_SUBJECT: 'dev-architect',
      ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL: 'true',
      ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL: `file://${repo}`,
      ARCHISTRATOR_CONSTRUCTION_DRYRUN: 'false',
      ARCHISTRATOR_STATE_MCP_BIN: stateTool,
      ARCHISTRATOR_LOCAL_EXEC_RUN_TIMEOUT: '10m',
      ...config,
    },
  });
  const s: Stack = { dir, repo, temporal, server };
  stack = s;
  await waitFor('the server answering /healthz', 120_000, async () => {
    if (server.exitCode !== null) {
      throw new Error(`uitests: the server exited ${String(server.exitCode)} (${dir}/server.log)`);
    }
    return httpOK(`http://127.0.0.1:${String(API_PORT)}/healthz`);
  });
  return s;
}

// ── The venue: the scripted agent ────────────────────────────────────────────
//
// `claude` on the server's PATH is a stub that hands its whole invocation (cwd,
// environment, argv) to this process over HTTP and ends with the exit code, stdout
// and stderr it gets back. This process runs the job as the scenario scripts it.

type Outcome = 'succeed' | 'fail' | 'hold';

type Job = {
  n: number;
  mode: string; // AIARCH_JOB_MODE: draft | critique | answer | construct
  command: string; // AIARCH_COMMAND
  activity: string; // AIARCH_ACTIVITY_ID (construct)
  component: string; // AIARCH_COMPONENT_ID (construct)
  kind: string; // AIARCH_ARTIFACT_KIND (design)
  note: string; // AIARCH_OPERATOR_NOTE: the operator's steer for this attempt, if any
  dir: string; // the job's worktree
  tool: { command: string; env: Record<string, string> };
  outcome?: Outcome;
};

type AgentReply = { exit: number; stdout: string; stderr: string };

const venue = {
  port: 0,
  jobs: [] as Job[],
  script: (() => 'succeed') as (job: Job) => Outcome,
  held: [] as (() => void)[],
};

let agentServer: http.Server | undefined;

async function startAgentServer(): Promise<number> {
  if (agentServer !== undefined) return venue.port;
  const srv = http.createServer((req, res) => {
    let body = '';
    req.setEncoding('utf8');
    req.on('data', (c: string) => (body += c));
    req.on('end', () => {
      void handleAgent(body).then((reply) => {
        res.setHeader('content-type', 'application/json');
        res.end(JSON.stringify(reply));
      });
    });
  });
  srv.requestTimeout = 0;
  srv.headersTimeout = 0;
  await new Promise<void>((resolve) => srv.listen(0, '127.0.0.1', () => resolve()));
  const addr = srv.address();
  if (addr === null || typeof addr !== 'object')
    throw new Error('uitests: the venue has no address');
  agentServer = srv;
  venue.port = addr.port;
  return venue.port;
}

/**
 * Prepare this scenario's venue directory: `claude` (the stub) and nothing else.
 * The stub answers `--version` itself (the server's boot preflight runs it).
 */
async function openVenue(dir: string): Promise<string> {
  const port = await startAgentServer();
  for (const release of venue.held.splice(0)) release();
  venue.jobs = [];
  venue.script = () => 'succeed';
  const venueDir = path.join(dir, 'venue');
  fs.mkdirSync(venueDir, { recursive: true });
  const stub = `#!${process.execPath}
'use strict';
// The venue's \`claude\`: the scripted agent's stub (uitests/generated/webClient/hooks.ts).
const http = require('node:http');
const argv = process.argv.slice(2);
if (argv.includes('--version')) {
  process.stdout.write('0.0.0 (scripted agent)\\n');
  process.exit(0);
}
const body = JSON.stringify({ cwd: process.cwd(), env: process.env, argv });
const req = http.request(
  { host: '127.0.0.1', port: ${String(port)}, path: '/agent', method: 'POST', headers: { 'content-type': 'application/json' } },
  (res) => {
    let b = '';
    res.setEncoding('utf8');
    res.on('data', (c) => (b += c));
    res.on('end', () => {
      const r = JSON.parse(b);
      process.stdout.write(r.stdout || '');
      process.stderr.write(r.stderr || '');
      process.exitCode = r.exit;
    });
  }
);
req.on('error', (e) => {
  process.stderr.write('scripted agent: no scenario is running: ' + e.message + '\\n');
  process.exitCode = 1;
});
req.end(body);
`;
  fs.writeFileSync(path.join(venueDir, 'claude'), stub, { mode: 0o755 });
  return venueDir;
}

/** The stream-json a run writes: its init and its terminal result (what the venue mines). */
function streamEvents(failed: boolean): string {
  const init = { type: 'system', subtype: 'init', model: 'scripted-agent' };
  const result = {
    type: 'result',
    subtype: failed ? 'error_during_execution' : 'success',
    is_error: failed,
    num_turns: 1,
    total_cost_usd: 0,
    usage: { input_tokens: 0, output_tokens: 0 },
  };
  return `${JSON.stringify(init)}\n${JSON.stringify(result)}\n`;
}

async function handleAgent(body: string): Promise<AgentReply> {
  const req = JSON.parse(body) as { cwd: string; env: Record<string, string>; argv: string[] };
  const cfgAt = req.argv.indexOf('--mcp-config');
  const mode = req.env['AIARCH_JOB_MODE'];
  if (cfgAt < 0 || mode === undefined) {
    return {
      exit: 1,
      stdout: '',
      stderr: `scripted agent: not a venue job (argv ${req.argv.join(' ')})`,
    };
  }
  // The venue passes the config path relative to the job's worktree ("../aiarch-mcp.json",
  // outside the git dir), the way claude resolves it against its own cwd.
  const cfgPath = path.resolve(req.cwd, req.argv[cfgAt + 1] ?? '');
  const cfg = JSON.parse(fs.readFileSync(cfgPath, 'utf8')) as {
    mcpServers: Record<string, { command: string; env?: Record<string, string> }>;
  };
  const tool = cfg.mcpServers['aiarch-state'];
  if (tool === undefined) {
    return {
      exit: 1,
      stdout: '',
      stderr: 'scripted agent: no aiarch-state server in the MCP config',
    };
  }
  const job: Job = {
    n: venue.jobs.length,
    mode,
    command: req.env['AIARCH_COMMAND'] ?? '',
    activity: req.env['AIARCH_ACTIVITY_ID'] ?? '',
    component: req.env['AIARCH_COMPONENT_ID'] ?? '',
    kind: req.env['AIARCH_ARTIFACT_KIND'] ?? '',
    note: req.env['AIARCH_OPERATOR_NOTE'] ?? '',
    dir: req.cwd,
    tool: { command: tool.command, env: tool.env ?? {} },
  };
  venue.jobs.push(job);
  job.outcome = venue.script(job);
  const steer = job.note === '' ? '' : ` (steer: ${JSON.stringify(job.note.slice(0, 80))})`;
  console.log(
    `[venue] job ${String(job.n)} ${job.mode} ${job.command} ${job.kind}${job.activity}${steer} -> ${job.outcome}`
  );
  if (job.outcome === 'fail') {
    return { exit: 1, stdout: streamEvents(true), stderr: 'scripted agent: the run failed' };
  }
  if (job.outcome === 'hold') {
    await new Promise<void>((resolve) => venue.held.push(resolve));
    return { exit: 1, stdout: '', stderr: 'scripted agent: still running when the scenario ended' };
  }
  try {
    await perform(job);
  } catch (e) {
    console.log(`[venue] job ${String(job.n)} could not do its work: ${String(e)}`);
    return { exit: 1, stdout: streamEvents(true), stderr: `scripted agent: ${String(e)}` };
  }
  return { exit: 0, stdout: streamEvents(false), stderr: '' };
}

// ── What a succeeding run does in its worktree ───────────────────────────────

type Doc = {
  slots?: Record<
    string,
    { model?: unknown; reviewThread?: { id: string; type?: string; status?: string }[] }
  >;
  serviceContracts?: Record<string, unknown>;
};

let design: Doc | undefined;
function designState(): Doc {
  design ??= JSON.parse(fs.readFileSync(DESIGN_STATE, 'utf8')) as Doc;
  return design;
}

/** The artifact kinds in slot order, by wire name. */
const SLOT_KINDS = [
  'mission',
  'glossary',
  'scrubbedRequirements',
  'volatilities',
  'coreUseCases',
  'system',
  'operationalConcepts',
  'standardCheck',
  'planningAssumptions',
  'activityList',
  'network',
  'normalSolution',
  'subcriticalSolution',
  'compressedSolution',
  'decompressedSolution',
  'riskModel',
  'sdpReview',
];

/** The slot key of an artifact kind named by its wire name or its PascalCase name. */
function slotKey(kind: string): string {
  const i = SLOT_KINDS.findIndex((k) => k.toLowerCase() === kind.toLowerCase());
  if (i < 0) throw new Error(`unknown artifact kind ${kind}`);
  return String(i);
}

function worktreeDoc(job: Job): Doc {
  const p = path.join(job.dir, '.aiarch/state/project.json');
  return fs.existsSync(p) ? (JSON.parse(fs.readFileSync(p, 'utf8')) as Doc) : {};
}

async function perform(job: Job): Promise<void> {
  switch (job.mode) {
    case 'draft':
      return draft(job);
    case 'critique':
      return withStateTool(job, async (s) => {
        await s.call('setCritiqueVerdict', { verdict: 'approve', notes: '' });
      });
    case 'answer':
      return answer(job);
    case 'construct':
      return construct(job);
  }
  throw new Error(`no script for job mode ${job.mode}`);
}

/**
 * A draft. A first draft authors the slot as the app's own design state holds it; a
 * redraft (the worktree already holds the slot: a send-back's, or an amendment's)
 * revises what is there, so the redraft is a change and not a re-affirmation.
 */
async function draft(job: Job): Promise<void> {
  const key = slotKey(job.kind);
  const held = worktreeDoc(job).slots?.[key]?.model;
  const model =
    held === undefined || held === null ? designState().slots?.[key]?.model : revise(held);
  if (model === undefined || model === null) throw new Error(`no ${job.kind} model to draft`);
  await withStateTool(job, async (s) => {
    await s.call('putDraftModel', { model });
  });
}

/** The model with its first prose string (a string with a space in it) revised. */
function revise(model: unknown): unknown {
  let done = false;
  const walk = (v: unknown): unknown => {
    if (done) return v;
    if (typeof v === 'string') {
      if (!v.includes(' ')) return v;
      done = true;
      return `${v} (revised)`;
    }
    if (Array.isArray(v)) return v.map(walk);
    if (v !== null && typeof v === 'object') {
      return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, walk(x)]));
    }
    return v;
  };
  return walk(model);
}

/** An answer: a reply to every open question on the slot's thread. */
async function answer(job: Job): Promise<void> {
  const thread = worktreeDoc(job).slots?.[slotKey(job.kind)]?.reviewThread ?? [];
  await withStateTool(job, async (s) => {
    for (const c of thread) {
      if (c.type !== 'question' || c.status !== 'open') continue;
      await s.call('respondToReviewComment', {
        id: c.id,
        response:
          'Per project: an objective is a business outcome of the whole system, not of one activity.',
      });
    }
  });
}

/** A construction job: the component's contract at detailed design, else a change on its branch. */
async function construct(job: Job): Promise<void> {
  if (job.command.endsWith('-detailed-design')) {
    const contracts = designState().serviceContracts ?? {};
    const fold = (s: string): string => s.toLowerCase().replace(/[^a-z0-9]/g, '');
    const key = Object.keys(contracts).find((k) => fold(k) === fold(job.component));
    if (key !== undefined) {
      await withStateTool(job, async (s) => {
        await s.call('recordServiceContract', { contract: contracts[key] });
      });
      return;
    }
  }
  const rel = path.join('work', job.activity, `${job.command}.md`);
  fs.mkdirSync(path.join(job.dir, path.dirname(rel)), { recursive: true });
  fs.writeFileSync(
    path.join(job.dir, rel),
    `${job.command} for ${job.activity}, run ${String(job.n + 1)}\n`
  );
  git(job.dir, 'add', rel);
  git(job.dir, 'commit', '-q', '-m', `${job.command} ${job.activity}`);
}

type RpcReply = { result?: unknown; error?: { message: string } };

/** An MCP client over the state tool's stdio: what the agent's session does with it. */
class StateSession {
  private next = 0;
  private buf = '';
  private stderr = '';
  private readonly waiting = new Map<number, (msg: RpcReply) => void>();
  private readonly child: ChildProcess;

  private constructor(child: ChildProcess) {
    this.child = child;
    child.stdout?.setEncoding('utf8');
    child.stdout?.on('data', (chunk: string) => {
      this.buf += chunk;
      let nl = this.buf.indexOf('\n');
      while (nl >= 0) {
        this.onLine(this.buf.slice(0, nl));
        this.buf = this.buf.slice(nl + 1);
        nl = this.buf.indexOf('\n');
      }
    });
    child.stderr?.setEncoding('utf8');
    child.stderr?.on('data', (chunk: string) => (this.stderr += chunk));
  }

  static async open(job: Job): Promise<StateSession> {
    const child = spawn(job.tool.command, [], {
      cwd: job.dir,
      env: { PATH: process.env['PATH'] ?? '', HOME: process.env['HOME'] ?? '', ...job.tool.env },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    const s = new StateSession(child);
    await s.request('initialize', {
      protocolVersion: '2025-06-18',
      capabilities: {},
      clientInfo: { name: 'scripted-agent', version: '1' },
    });
    s.send({ jsonrpc: '2.0', method: 'notifications/initialized' });
    return s;
  }

  private onLine(line: string): void {
    let msg: RpcReply & { id?: number };
    try {
      msg = JSON.parse(line) as RpcReply & { id?: number };
    } catch {
      return;
    }
    if (msg.id === undefined) return;
    const w = this.waiting.get(msg.id);
    if (w === undefined) return;
    this.waiting.delete(msg.id);
    w(msg);
  }

  private send(msg: unknown): void {
    this.child.stdin?.write(`${JSON.stringify(msg)}\n`);
  }

  request(method: string, params: unknown): Promise<unknown> {
    const id = ++this.next;
    return new Promise((resolve, reject) => {
      const onExit = (): void =>
        reject(new Error(`${method}: the state tool exited (stderr: ${this.stderr})`));
      this.child.once('exit', onExit);
      this.waiting.set(id, (msg) => {
        this.child.off('exit', onExit);
        if (msg.error !== undefined) reject(new Error(`${method}: ${msg.error.message}`));
        else resolve(msg.result);
      });
      this.send({ jsonrpc: '2.0', id, method, params });
    });
  }

  /** Invoke one tool; a result flagged isError is the tool refusing. */
  async call(tool: string, args: Record<string, unknown>): Promise<string> {
    const res = (await this.request('tools/call', { name: tool, arguments: args })) as {
      isError?: boolean;
      content?: { text?: string }[];
    };
    const text = (res.content ?? []).map((c) => c.text ?? '').join('');
    if (res.isError === true) throw new Error(`${tool} refused: ${text}`);
    return text;
  }

  async close(): Promise<void> {
    if (this.child.exitCode !== null) return;
    const exited = new Promise<void>((resolve) => this.child.once('exit', () => resolve()));
    this.child.stdin?.end();
    await exited;
  }
}

/** Run body against the job's state tool, then publish: the last thing every state-writing run does. */
async function withStateTool(job: Job, body: (s: StateSession) => Promise<void>): Promise<void> {
  const s = await StateSession.open(job);
  try {
    await body(s);
    await s.call('publishDraft', { message: `scripted agent: ${job.command}` });
  } finally {
    await s.close();
  }
}

// ── Driving the server as its actors ─────────────────────────────────────────

const API = `http://127.0.0.1:${String(API_PORT)}/api/v1`;

async function post(page: Page, route: string, data: unknown): Promise<unknown> {
  const res = await page.request.post(`${API}/${route}`, { data });
  const text = await res.text();
  if (!res.ok())
    throw new Error(`uitests: POST ${route} refused (${String(res.status())}): ${text}`);
  return text === '' ? null : (JSON.parse(text) as unknown);
}

type TaskView = { id: string; state: string; revisions?: unknown[] };
type ActivityView = { state: string; tasks?: TaskView[] };

async function activityView(page: Page, activity: string): Promise<ActivityView | undefined> {
  const res = await page.request.get(
    `${API}/delivery/query-activity-view/${PROJECT_PATH}/${activity}`
  );
  if (!res.ok()) return undefined;
  return (await res.json()) as ActivityView;
}

async function taskView(page: Page, activity: string, id: string): Promise<TaskView | undefined> {
  return (await activityView(page, activity))?.tasks?.find((t) => t.id === id);
}

/** Wait until the activity's task reads state (and, when given, holds that many revisions). */
async function awaitTask(
  page: Page,
  activity: string,
  id: string,
  state: string,
  revisions?: number
): Promise<void> {
  const want = revisions === undefined ? state : `${state}/${String(revisions)}`;
  await expect
    .poll(
      async () => {
        const t = await taskView(page, activity, id);
        if (t === undefined) return '';
        return revisions === undefined ? t.state : `${t.state}/${String(t.revisions?.length ?? 0)}`;
      },
      { timeout: WALK_WINDOW_MS, intervals: [1_000], message: `${activity}/${id} reaching ${want}` }
    )
    .toBe(want);
}

/** Wait until the activity reads state (or a state the pattern matches). */
async function awaitActivity(page: Page, activity: string, state: string | RegExp): Promise<void> {
  const want = typeof state === 'string' ? new RegExp(`^${state}$`) : state;
  await expect
    .poll(async () => (await activityView(page, activity))?.state ?? '', {
      timeout: WALK_WINDOW_MS,
      intervals: [1_000],
      message: `${activity} reaching ${String(state)}`,
    })
    .toMatch(want);
}

type ProjectSummary = {
  PhaseName?: string;
  Slots?: { kind: string; stage: number; staleBasis?: boolean }[];
};

/** The project as the owner's screens read it. */
async function projectSummary(page: Page): Promise<ProjectSummary | undefined> {
  const res = await page.request.post(`${API}/delivery/query-project-view`, {
    data: { query: { kind: 'summary', projectId: PROJECT_ID } },
  });
  if (!res.ok()) return undefined;
  return ((await res.json()) as { summary?: ProjectSummary }).summary;
}

/** Wait until the project's slot of kind reads a stale basis (want 'stale') or no longer does ('current'). */
async function awaitStale(page: Page, kind: string, want: 'stale' | 'current'): Promise<void> {
  await expect
    .poll(
      async () => {
        const summary = await projectSummary(page);
        if (summary === undefined) return 'unread';
        return summary.Slots?.find((s) => s.kind === kind)?.staleBasis === true
          ? 'stale'
          : 'current';
      },
      {
        timeout: WALK_WINDOW_MS,
        intervals: [1_000],
        message: want === 'stale' ? `${kind} reading a stale basis` : `${kind} no longer stale`,
      }
    )
    .toBe(want);
}

/** The research the app's own design state was drafted from. */
function research(): { sources: { title: string; content: string }[] } {
  const doc = JSON.parse(fs.readFileSync(DESIGN_STATE, 'utf8')) as {
    research?: { sources?: { title: string; path: string }[] };
  };
  return {
    sources: (doc.research?.sources ?? []).map((s) => ({
      title: s.title,
      content: fs.readFileSync(path.join(REPO_ROOT, s.path), 'utf8'),
    })),
  };
}

/** The customer starts the project: born, then started with its research. */
async function startProject(page: Page): Promise<void> {
  await post(page, 'delivery/start-project', {
    owner: PROJECT_OWNER,
    name: PROJECT_ID,
    start: false,
  });
  await post(page, 'delivery/start-project', {
    owner: PROJECT_OWNER,
    name: PROJECT_ID,
    projectID: PROJECT_ID,
    research: research(),
    start: true,
  });
}

/** The human approves the gate the walk holds at. */
async function approve(
  page: Page,
  activity: string,
  taskID: string,
  optionId?: string
): Promise<void> {
  await awaitTask(page, activity, taskID, 'awaitingHuman');
  await post(page, `delivery/submit-review-decision/${PROJECT_PATH}/${activity}`, {
    taskID,
    decision: optionId === undefined ? { decision: 1 } : { decision: 1, optionId },
  });
  await awaitTask(page, activity, taskID, 'passed');
}

/** The design walk's human gates, in walk order. */
const DESIGN_GATES: [string, string][] = [
  ['requirements', 'missionReview'],
  ['requirements', 'glossaryReview'],
  ['requirements', 'volatilitiesReview'],
  ['requirements', 'coreUseCasesReview'],
  ['architecture', 'architectureReview'],
];

/** Walk the design from the project's birth: every gate up to (not including) `stopAt` approved. */
async function walkDesign(page: Page, stopAt: string): Promise<void> {
  await startProject(page);
  for (const [activity, gate] of DESIGN_GATES) {
    if (gate === stopAt) {
      await awaitTask(page, activity, gate, 'awaitingHuman');
      return;
    }
    await approve(page, activity, gate);
  }
  await awaitTask(page, 'projectDesign', 'sdpReview', 'awaitingHuman');
}

/** The project's review policy, set by the owner (the op the SPA's policy picker calls). */
async function setPolicy(page: Page, policy: unknown): Promise<void> {
  await post(page, `delivery/set-project-execution-policy/${PROJECT_PATH}`, { policy });
}

type SdpReview = { optionId: string; solutionKind?: string }[];

/** The options the M0 review presents, read as the reviewer's screen reads them. */
async function presentedOptions(page: Page): Promise<SdpReview> {
  const res = await page.request.post(`${API}/delivery/query-project-view`, {
    data: {
      query: { kind: 'session', projectId: PROJECT_ID, artifactKind: Number(slotKey('sdpReview')) },
    },
  });
  if (!res.ok()) return [];
  const view = (await res.json()) as {
    projectSession?: { draft?: { model?: { options?: SdpReview } } };
    session?: { draft?: { model?: { options?: SdpReview } } };
  };
  return (view.projectSession ?? view.session)?.draft?.model?.options ?? [];
}

/**
 * The design walk to M0, the human approving whichever design gate the policy holds
 * for them on the way.
 */
async function walkToM0(page: Page): Promise<void> {
  const deadline = Date.now() + ARRANGE_BUDGET_MS / 2;
  while (Date.now() < deadline) {
    if ((await taskView(page, 'projectDesign', 'sdpReview'))?.state === 'awaitingHuman') return;
    for (const activity of ['requirements', 'architecture']) {
      const gate = (await activityView(page, activity))?.tasks?.find(
        (t) => t.state === 'awaitingHuman'
      );
      if (gate !== undefined) await approve(page, activity, gate.id);
    }
    await new Promise((r) => setTimeout(r, 1_000));
  }
  throw new Error('uitests: the design walk did not reach M0');
}

/** The human commits the plan at M0: the normal solution, as the review presents it. */
async function commitPlan(page: Page): Promise<void> {
  await walkToM0(page);
  const options = await presentedOptions(page);
  const normal = options.find((o) => /normal/i.test(o.solutionKind ?? o.optionId)) ?? options[0];
  if (normal === undefined) throw new Error('uitests: the M0 review presents no option');
  await approve(page, 'projectDesign', 'sdpReview', normal.optionId);
  // The gate reads passed a beat before the seal's own writes (the plan committed, the
  // project moved into construction); the owner's screen shows the seal once it landed.
  await expect
    .poll(
      async () => {
        const summary = await projectSummary(page);
        const plan = summary?.Slots?.find((s) => s.kind === 'activityList');
        return `${String(summary?.PhaseName)}/${String(plan?.stage)}`;
      },
      { timeout: WALK_WINDOW_MS, intervals: [1_000], message: 'the M0 seal landing' }
    )
    .toBe('construction/2');
  // ...and the M0 walk has exited (its outcome recorded): an amendment sent while the
  // walk is still finishing is addressed to its decided gate and refused.
  await awaitActivity(page, 'projectDesign', 'done');
}

/** The construction activity the execute scenarios drive. */
const DRIVEN = 'C-review-engine';

/**
 * The project in construction under `policy` (set at birth, so the design walk runs
 * under it too): designed by the agent, the plan committed at M0 by the human. Every
 * agent run of a construction activity other than DRIVEN stays in flight (the pump
 * starts them all; only DRIVEN is the scenario's), and DRIVEN's runs end as `script`
 * says.
 */
async function inConstruction(
  page: Page,
  policy: unknown,
  script: (job: Job) => Outcome
): Promise<void> {
  venue.script = (job) => {
    if (job.mode !== 'construct') return 'succeed';
    return job.activity === DRIVEN ? script(job) : 'hold';
  };
  await post(page, 'delivery/start-project', {
    owner: PROJECT_OWNER,
    name: PROJECT_ID,
    start: false,
  });
  if (policy !== undefined) await setPolicy(page, policy);
  await post(page, 'delivery/start-project', {
    owner: PROJECT_OWNER,
    name: PROJECT_ID,
    projectID: PROJECT_ID,
    research: research(),
    start: true,
  });
  await commitPlan(page);
}

/**
 * The operator's intervention policy for failed construction work: every failure is
 * escalated to the operator at once, a retry budget of none (the server's own
 * setting, ARCHISTRATOR_CONSTRUCTION_INTERVENTION_MODE). The default ("tiered")
 * retries and then takes the work over, so a failure there never reaches the operator.
 */
const ESCALATE_EVERYTHING = { ARCHISTRATOR_CONSTRUCTION_INTERVENTION_MODE: 'escalate-everything' };

/** How each scenario's server is configured, beyond the LOCAL profile. */
const CONFIG: Record<string, Record<string, string>> = {
  'execute-a-project-activity-P2': ESCALATE_EVERYTHING,
  'execute-a-project-activity-P6': ESCALATE_EVERYTHING,
};

/**
 * The operator's steer has been acted on: DRIVEN's construction has run `run` times
 * (the steer's run among them), and the task settled where a human (or nobody) is next.
 */
async function awaitSteered(page: Page, run: number): Promise<void> {
  await expect
    .poll(() => venue.jobs.filter((j) => j.activity === DRIVEN && isConstructionStep(j)).length, {
      timeout: WALK_WINDOW_MS,
      intervals: [1_000],
      message: `construction run ${String(run)} of ${DRIVEN}`,
    })
    .toBeGreaterThanOrEqual(run);
  await expect
    .poll(async () => (await taskView(page, DRIVEN, 'construction'))?.state ?? '', {
      timeout: WALK_WINDOW_MS,
      intervals: [1_000],
      message: `${DRIVEN}/construction settling after the steer`,
    })
    .toMatch(/^(passed|failed|awaitingHuman)$/);
}

/** Agents only: no gate asks a human. */
const AGENTS_ONLY = { preset: 'vibes' };
/** A human required at the code review (the construction phase's gate). */
const HUMAN_AT_CODE_REVIEW = {
  preset: '',
  policy: { gatedPhasesByType: { service: ['construction'] } },
};

const isConstructionStep = (job: Job): boolean => job.command.endsWith('-construction');

/** DRIVEN's first `n` construction attempts fail. */
function failTimes(n: number): (job: Job) => Outcome {
  let failed = 0;
  return (job) => {
    if (!isConstructionStep(job) || failed >= n) return 'succeed';
    failed++;
    return 'fail';
  };
}

/**
 * A starting state no actor of the venue can produce: the scenario fails here, naming
 * what is missing, rather than passing on a screen that only looks like its branch.
 */
function unproducible(why: string): Arrange {
  return () =>
    Promise.reject(
      new Error(`uitests: no actor can produce this scenario's starting state: ${why}`)
    );
}

const succeedAll = (): Outcome => 'succeed';
const holdAll = (): Outcome => 'hold';

// ── Starting states ──────────────────────────────────────────────────────────

type Arrange = (page: Page) => Promise<void>;

/** The requirements walk holding at the mission review for the human. */
async function missionGate(page: Page): Promise<void> {
  await startProject(page);
  await awaitTask(page, 'requirements', 'missionReview', 'awaitingHuman');
}

/**
 * The requirements committed (an activity's approved drafts merge onto main when the
 * activity completes); the walk holds at the architecture review.
 */
const requirementsCommitted: Arrange = (page) => walkDesign(page, 'architectureReview');

/** The design committed through the architecture; the walk holds at M0 for the human. */
const m0Gate: Arrange = (page) => walkDesign(page, 'sdpReview');

/**
 * The plan committed at M0 under the default policy (every design gate held for the
 * human, who approved each) and construction under way: every construction run is
 * still in flight when the owner looks.
 */
const planCommitted: Arrange = (page) => inConstruction(page, undefined, holdAll);

/** DRIVEN's construction ran and the activity settled where a human (or nobody) is next. */
function executing(policy: unknown, script: (job: Job) => Outcome): Arrange {
  return async (page) => {
    await inConstruction(page, policy, script);
    await awaitActivity(page, DRIVEN, /^(done|failed|awaitingHuman)$/);
  };
}

/** The addressee's answer job is never run: the venue misses its dispatch. */
const answerMissed = (job: Job): Outcome => (job.mode === 'answer' ? 'fail' : 'succeed');

/** A scenario's arrangement, then how the venue's agent runs end for the rest of it. */
function scripted(arrange: Arrange, script: (job: Job) => Outcome): Arrange {
  return async (page) => {
    await arrange(page);
    venue.script = script;
  };
}

const ARRANGE: Record<string, Arrange> = {
  'add-a-use-case-to-an-in-flight-project-P1': m0Gate,
  'add-a-use-case-to-an-in-flight-project-P2': m0Gate,
  'add-a-use-case-to-an-in-flight-project-P3': m0Gate,
  'add-a-use-case-to-an-in-flight-project-P4': m0Gate,
  'ask-a-clarifying-question-during-review-P1': missionGate,
  'ask-a-clarifying-question-during-review-P2': scripted(missionGate, answerMissed),
  'ask-a-clarifying-question-during-review-P3': requirementsCommitted,
  'ask-a-clarifying-question-during-review-P4': scripted(requirementsCommitted, answerMissed),
  'execute-a-project-activity-P1': executing(AGENTS_ONLY, succeedAll),
  // Escalate-everything (CONFIG): each failed run waits for the operator's steer, so
  // the run after the n-th failure is the n-th steer's.
  'execute-a-project-activity-P2': executing(AGENTS_ONLY, failTimes(1)),
  'execute-a-project-activity-P3': executing(AGENTS_ONLY, succeedAll),
  'execute-a-project-activity-P4': executing(HUMAN_AT_CODE_REVIEW, succeedAll),
  'execute-a-project-activity-P5': executing(AGENTS_ONLY, failTimes(1)),
  'execute-a-project-activity-P6': executing(AGENTS_ONLY, failTimes(2)),
  'execute-a-project-activity-P7': unproducible(
    "it needs the code review decided by its AGENT reviewers as a send-back; the service lifecycle's codeReview task (method-assets lifecycles.json) carries no command, so the review engine's agent roster is never run and an agents-only code review passes with no verdict at all: no agent can send construction back for a revision 2 (and the RevisionSelect test id renders for a single revision too, so the binding's observable would pass on revision 1)"
  ),
  'replan-under-scope-change-P1': planCommitted,
  'replan-under-scope-change-P2': planCommitted,
  'replan-under-scope-change-P3': planCommitted,
  // Every construction run fails; the default (tiered) intervention re-dispatches until
  // the supervision budget is spent and the activity exits failed.
  'requeue-a-failed-construction-activity-P1': executing(AGENTS_ONLY, failTimes(Infinity)),
  'send-back-change-requests-for-a-redraft-P1': missionGate,
  'send-back-change-requests-for-a-redraft-P2': missionGate,
};

// ── Settling a step ──────────────────────────────────────────────────────────
// What a step set in motion on the server, waited for before the spec observes it.

/** The amendment's redraft is back at the core use cases review. */
const amendedUseCasesAtGate = (page: Page): Promise<void> =>
  awaitTask(page, 'requirements', 'coreUseCasesReview', 'awaitingHuman', 2);

/**
 * The amendment approved: the requirements activity is done again, and its recommitted
 * use case model has landed on main — which is what marks the architecture's System
 * stale (downstream-artifacts-stale). The activity reads done the moment its gate passes,
 * a beat before the merge tail's commit, and the architecture screen of a done activity
 * is read once, so the spec must not look before the commit has landed.
 */
const amendedUseCasesCommitted = async (page: Page): Promise<void> => {
  await awaitTask(page, 'requirements', 'coreUseCasesReview', 'passed', 2);
  await awaitActivity(page, 'requirements', 'done');
  await awaitStale(page, 'system', 'stale');
};

/** The architecture's stale basis acknowledged. */
const architectureReconciled = (page: Page): Promise<void> => awaitStale(page, 'system', 'current');

const SETTLE: Record<string, Record<number, (page: Page) => Promise<void>>> = {
  'add-a-use-case-to-an-in-flight-project-P1': {
    1: amendedUseCasesAtGate,
    2: amendedUseCasesCommitted,
  },
  'add-a-use-case-to-an-in-flight-project-P2': {
    1: amendedUseCasesAtGate,
    2: amendedUseCasesCommitted,
    3: architectureReconciled,
  },
  'add-a-use-case-to-an-in-flight-project-P3': {
    1: amendedUseCasesAtGate,
    2: amendedUseCasesCommitted,
  },
  'add-a-use-case-to-an-in-flight-project-P4': {
    1: amendedUseCasesAtGate,
    2: amendedUseCasesCommitted,
    3: architectureReconciled,
  },
  // The operator's steer re-dispatched the construction; the run it started is over.
  'execute-a-project-activity-P2': { 2: (page) => awaitSteered(page, 2) },
  'execute-a-project-activity-P6': {
    2: (page) => awaitSteered(page, 2),
    3: (page) => awaitSteered(page, 3),
  },
  // The amendment's redraft is back at its gate: the architecture's, or the plan's
  // re-derivation at M0.
  'replan-under-scope-change-P1': {
    1: (page) => awaitTask(page, 'architecture', 'architectureReview', 'awaitingHuman', 2),
  },
  'replan-under-scope-change-P2': {
    1: (page) => awaitTask(page, 'projectDesign', 'sdpReview', 'awaitingHuman', 2),
  },
  'replan-under-scope-change-P3': {
    1: (page) => awaitTask(page, 'projectDesign', 'sdpReview', 'awaitingHuman', 2),
  },
  // The redraft the send-back asked for is back at the gate.
  'send-back-change-requests-for-a-redraft-P1': {
    2: (page) => awaitTask(page, 'requirements', 'missionReview', 'awaitingHuman', 2),
  },
  'send-back-change-requests-for-a-redraft-P2': {
    2: (page) => awaitTask(page, 'requirements', 'missionReview', 'awaitingHuman', 2),
    4: (page) => awaitTask(page, 'requirements', 'missionReview', 'awaitingHuman', 3),
  },
};

export const hooks = {
  async before(id: string, page: Page): Promise<void> {
    const arrange = ARRANGE[id];
    if (arrange === undefined) throw new Error(`uitests: no arrangement for scenario ${id}`);
    // Bringing a stack up and walking it to the scenario's starting state is not the
    // scenario: its time is added to the scenario's budget rather than spent out of it.
    const budget = test.info().timeout;
    const started = Date.now();
    test.setTimeout(budget + ARRANGE_BUDGET_MS);
    await bootStack(id, CONFIG[id] ?? {});
    await arrange(page);
    test.setTimeout(budget + (Date.now() - started));
  },
  async step(id: string, seq: number, page: Page): Promise<void> {
    const settle = SETTLE[id]?.[seq];
    if (settle === undefined) return;
    // Waiting on the agent runs a step set in motion is the venue's time, not the
    // scenario's: it is added to the budget too.
    const budget = test.info().timeout;
    const started = Date.now();
    test.setTimeout(budget + WALK_WINDOW_MS);
    await settle(page);
    test.setTimeout(budget + (Date.now() - started));
  },
};
