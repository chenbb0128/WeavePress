import type {
  AIJob,
  AIJobStatus,
  AIJobType,
  AITone,
  GenerationInput,
} from '#/api/ai';

export interface GenerationValidationError {
  field: keyof GenerationInput;
  message: string;
}

export const AI_JOB_STATUS_LABELS: Record<AIJobStatus, string> = {
  completed: '已完成',
  failed: '失败',
  queued: '排队中',
  running: '执行中',
};

export const AI_JOB_TYPE_LABELS: Record<AIJobType, string> = {
  analysis: '文章分析',
  generation: '稿件生成',
};

const ALLOWED_TONES: ReadonlySet<string> = new Set<AITone>([
  'analytical',
  'plain',
  'professional',
  'storytelling',
  'warm',
]);

function characterCount(value: string) {
  return [...value].length;
}

function isASCII(value: string) {
  return [...value].every(
    (character) => (character.codePointAt(0) ?? 128) <= 127,
  );
}

function utf8ByteLength(value: string) {
  return new TextEncoder().encode(value).length;
}

export function validateGenerationForm(
  input: GenerationInput,
): GenerationValidationError[] {
  const errors: GenerationValidationError[] = [];

  if (!input.angleId.trim()) {
    errors.push({ field: 'angleId', message: '请选择文章角度' });
  }

  if (!input.audience.trim()) {
    errors.push({ field: 'audience', message: '请输入目标读者' });
  } else if (characterCount(input.audience) > 100) {
    errors.push({
      field: 'audience',
      message: '目标读者不能超过 100 字',
    });
  }

  if (!ALLOWED_TONES.has(input.tone)) {
    errors.push({ field: 'tone', message: '请选择有效语气' });
  }

  if (
    !Number.isInteger(input.targetWords) ||
    input.targetWords < 300 ||
    input.targetWords > 5000
  ) {
    errors.push({
      field: 'targetWords',
      message: '目标字数应在 300 到 5000 之间',
    });
  }

  if (characterCount(input.additionalInstructions ?? '') > 500) {
    errors.push({
      field: 'additionalInstructions',
      message: '补充要求不能超过 500 字',
    });
  }

  const idempotencyKeyLength = utf8ByteLength(input.idempotencyKey);
  if (idempotencyKeyLength < 8 || idempotencyKeyLength > 128) {
    errors.push({
      field: 'idempotencyKey',
      message: '幂等键长度应在 8 到 128 个字符之间',
    });
  } else if (!isASCII(input.idempotencyKey)) {
    errors.push({
      field: 'idempotencyKey',
      message: '幂等键只能包含 ASCII 字符',
    });
  }

  return errors;
}

export function shouldPoll(status: AIJobStatus) {
  return status === 'queued' || status === 'running';
}

export function canGenerate(
  analysisStatus: AIJobStatus,
  input: GenerationInput,
) {
  return (
    analysisStatus === 'completed' && validateGenerationForm(input).length === 0
  );
}

export function canRetry(job: Pick<AIJob, 'retryable' | 'status'>) {
  return job.status === 'failed' && job.retryable;
}
