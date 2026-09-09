import type { PageResult } from './content';

import { requestClient } from '#/api/request';

export type AIJobStatus = 'completed' | 'failed' | 'queued' | 'running';
export type AIJobType = 'analysis' | 'generation';
export type AITone =
  | 'analytical'
  | 'plain'
  | 'professional'
  | 'storytelling'
  | 'warm';

export interface AIStatus {
  enabled: boolean;
  model: string;
  provider: string;
}

export interface AIFact {
  confidence: 'high' | 'low' | 'medium';
  id: string;
  sourceBlockIds: string[];
  text: string;
}

export interface AIViewpoint {
  holder: string;
  id: string;
  sourceBlockIds: string[];
  text: string;
}

export interface AIQuote {
  id: string;
  sourceBlockId: string;
  text: string;
}

export interface AIRisk {
  id: string;
  sourceBlockIds: string[];
  text: string;
}

export interface AIAngle {
  id: string;
  outline: string[];
  thesis: string;
  title: string;
}

export interface AIJobArticle {
  author?: string;
  canonicalUrl?: string;
  cleanHtml?: string;
  createdAt: string;
  createdBy: number;
  duplicateOfId?: null | number;
  id: number;
  language?: string;
  originalUrl?: string;
  plainText?: string;
  publishedAt?: null | string;
  sourceName?: string;
  sourceType: string;
  status: string;
  title: string;
  updatedAt: string;
}

export interface AIJobEvent {
  createdAt: string;
  id: number;
  jobId: number;
  message: string;
  status: string;
}

export interface AIJob {
  article?: AIJobArticle;
  articleId: number;
  attempts: number;
  createdAt: string;
  errorCode?: string;
  errorMessage?: string;
  events?: AIJobEvent[];
  finishedAt?: null | string;
  id: number;
  inputTokens: number;
  manualRetries: number;
  model: string;
  outputTokens: number;
  parentJobId?: null | number;
  promptVersion: string;
  provider: string;
  requestedBy: number;
  retryable: boolean;
  startedAt?: null | string;
  status: AIJobStatus;
  totalTokens: number;
  type: AIJobType;
  updatedAt: string;
}

export interface AIAnalysis {
  angles: AIAngle[];
  articleId: number;
  createdAt: string;
  facts: AIFact[];
  id: number;
  job?: AIJob;
  jobId: number;
  quotes: AIQuote[];
  risks: AIRisk[];
  summary: string;
  viewpoints: AIViewpoint[];
}

export interface AIGeneratedBlock {
  alt?: string;
  assetId?: number;
  factIds?: string[];
  items?: string[];
  level?: number;
  quoteId?: string;
  text?: string;
  type: 'heading' | 'image' | 'list' | 'paragraph' | 'quote';
}

export interface AIGeneration {
  additionalInstructions: string;
  analysisId: number;
  angleId: string;
  audience: string;
  blocks: AIGeneratedBlock[] | null;
  contentHtml?: string;
  createdAt: string;
  digest: string;
  draftId?: null | number;
  factMap?: Record<string, string[]>;
  id: number;
  job?: AIJob;
  jobId: number;
  targetWords: number;
  title: string;
  tone: AITone;
  updatedAt: string;
}

export interface GenerationInput {
  additionalInstructions?: string;
  angleId: string;
  audience: string;
  idempotencyKey: string;
  targetWords: number;
  tone: AITone;
}

export interface StartAnalysisResult {
  job: AIJob;
  reused: boolean;
}

export interface StartGenerationResult {
  generation: AIGeneration;
  job: AIJob;
  reused: boolean;
}

export function getAIStatusApi() {
  return requestClient.get<AIStatus>('/ai/status');
}

export function startAIAnalysisApi(articleId: number, force = false) {
  return requestClient.post<StartAnalysisResult>(
    `/articles/${articleId}/ai-analyses`,
    { force },
  );
}

export function getAIAnalysesApi(
  articleId: number,
  params: { page: number; pageSize: number },
) {
  return requestClient.get<PageResult<AIAnalysis>>(
    `/articles/${articleId}/ai-analyses`,
    { params },
  );
}

export function getAIAnalysisApi(id: number) {
  return requestClient.get<AIAnalysis>(`/ai-analyses/${id}`);
}

export function startAIGenerationApi(
  analysisId: number,
  input: GenerationInput,
) {
  return requestClient.post<StartGenerationResult>(
    `/ai-analyses/${analysisId}/generations`,
    input,
  );
}

export function getAIGenerationApi(id: number) {
  return requestClient.get<AIGeneration>(`/ai-generations/${id}`);
}

export function getAIJobsApi(params: {
  articleId?: number;
  page: number;
  pageSize: number;
  status?: AIJobStatus;
  type?: AIJobType;
}) {
  return requestClient.get<PageResult<AIJob>>('/ai-jobs', { params });
}

export function getAIJobApi(id: number) {
  return requestClient.get<AIJob>(`/ai-jobs/${id}`);
}

export function retryAIJobApi(id: number) {
  return requestClient.post<AIJob>(`/ai-jobs/${id}/retry`);
}
