import { useCallback, useEffect, useRef, useState, type SetStateAction } from "react";
import { Alert } from "react-native";
import type { ConnectionState } from "../../store/workers";
import { AttachmentUploadQueue } from "../../services/attachmentUploadQueue";
import { browserFileAsset } from "../../services/browserFileAsset";
import { createAttachmentUploadOperation, pickUploadDocuments, resolveServerUploadTarget, MAX_COMPOSER_ATTACHMENTS, type ActiveAttachmentUpload, type UploadDocumentAsset } from "../../services/uploads";
import type { ComposerAttachment } from "./InterfaceChatSession";

interface UseInterfaceComposerAttachmentsInput {
  serverId: string;
  ownerKey: string;
  attachments: ComposerAttachment[];
  connectionState: ConnectionState;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  focusComposer(): void;
}

export function useInterfaceComposerAttachments({ serverId, ownerKey, attachments, connectionState, setAttachments, focusComposer }: UseInterfaceComposerAttachmentsInput) {
  const queueRef = useRef<AttachmentUploadQueue | null>(null);
  const selectionGenerationRef = useRef(0);
  const attachmentsRef = useRef(attachments);
  attachmentsRef.current = attachments;
  const [selecting, setSelecting] = useState(false);
  const selectingRef = useRef(false);
  const uploading = selecting || attachments.some((item) => item.uploadStatus && item.uploadStatus !== "ready");
  const canAttach = connectionState === "connected" && !selecting && attachments.length < MAX_COMPOSER_ATTACHMENTS;

  useEffect(() => {
    const generation = ++selectionGenerationRef.current;
    setSelecting(false);
    selectingRef.current = false;
    const queue = new AttachmentUploadQueue(async (asset, onProgress) => {
      const target = await resolveServerUploadTarget(serverId);
      if (selectionGenerationRef.current !== generation) throw new Error("The upload owner changed.");
      return createAttachmentUploadOperation(asset, target, { onProgress });
    }, (items) => {
      const byId = new Map(items.map((item) => [item.id, item]));
      setAttachments((current) => current.map((attachment) => {
        const item = byId.get(attachment.id);
        return item ? { ...attachment, ...item.result, uploadStatus: item.status, uploadProgress: item.progress, uploadError: item.error } : attachment;
      }));
    });
    queueRef.current = queue;
    return () => {
      selectionGenerationRef.current++;
      queueRef.current = null;
      queue.dispose();
      setAttachments((current) => current.filter((item) => !item.uploadStatus || item.uploadStatus === "ready"));
    };
  }, [serverId, ownerKey, setAttachments]);

  useEffect(() => { queueRef.current?.forgetCompleted(new Set(attachments.map((item) => item.id))); }, [attachments]);

  // Queue picked, pasted or dropped files as composer attachments.
  const addAssets = useCallback((assets: UploadDocumentAsset[]) => {
    if (!assets.length) return;
    if (assets.length + attachmentsRef.current.length > MAX_COMPOSER_ATTACHMENTS) throw new Error("Remove an attachment before adding more files.");
    const entries = assets.map((asset) => ({ id: `${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}`, asset }));
    const placeholders: ComposerAttachment[] = entries.map(({ id, asset }) => ({
      id, name: asset.name || "upload", path: "", localUri: asset.uri, mimeType: asset.mimeType,
      uploadStatus: asset.selectionError ? "failed" : "queued", uploadError: asset.selectionError,
      retryUpload: asset.selectionRetryable === false ? undefined : () => queueRef.current?.retry(id),
    }));
    setAttachments((current) => [...current, ...placeholders]);
    queueRef.current?.enqueue(entries);
    focusComposer();
  }, [focusComposer, setAttachments]);

  const handleUploadAttachment = useCallback(async () => {
    if (!canAttach || selectingRef.current) return;
    const remaining = MAX_COMPOSER_ATTACHMENTS - attachmentsRef.current.length;
    const generation = selectionGenerationRef.current;
    selectingRef.current = true;
    setSelecting(true);
    try {
      const assets = await pickUploadDocuments(remaining);
      if (selectionGenerationRef.current !== generation) return;
      addAssets(assets);
    } catch (error) {
      if (selectionGenerationRef.current === generation) Alert.alert("Could not attach files", error instanceof Error ? error.message : "Try selecting the files again.");
    } finally {
      if (selectionGenerationRef.current === generation) { selectingRef.current = false; setSelecting(false); }
    }
  }, [addAssets, canAttach, setAttachments]);

  // Web: files pasted from the clipboard or dropped on the chat. The same
  // upload queue and backend as the picker; `file` is the browser's File.
  const addFiles = useCallback((files: File[]) => {
    if (!canAttach || !files.length) return;
    try {
      addAssets(files.map((file) => browserFileAsset(file)));
    } catch (error) {
      Alert.alert("Could not attach files", error instanceof Error ? error.message : "Try adding the files again.");
    }
  }, [addAssets, canAttach]);

  const removeAttachment = useCallback((id: string) => {
    queueRef.current?.remove(id);
    setAttachments((current) => current.filter((attachment) => attachment.id !== id));
  }, [setAttachments]);
  const cancelUpload = useCallback(() => {
    for (const item of attachmentsRef.current) if (item.uploadStatus && item.uploadStatus !== "ready") removeAttachment(item.id);
  }, [removeAttachment]);

  return { activeUpload: null as ActiveAttachmentUpload | null, addFiles, canAttach, cancelUpload, handleUploadAttachment, removeAttachment, uploading };
}
