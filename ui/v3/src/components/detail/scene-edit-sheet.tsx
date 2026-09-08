import { useQuery } from "@apollo/client/react";
import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import { EntityEditSheet } from "./entity-edit-sheet";
import { SceneEditForm } from "./scene-edit-form";

interface SceneEditSheetProps {
  id: string | null;
  onClose: () => void;
}

export function SceneEditSheet({ id, onClose }: SceneEditSheetProps) {
  const intl = useIntl();
  const { data, loading } = useQuery(GQL.FindSceneDocument, {
    variables: { id: id ?? "" },
    skip: !id,
  });

  const scene = data?.findScene;

  return (
    <EntityEditSheet
      open={!!id}
      onClose={onClose}
      entityType={intl.formatMessage({ id: "scene", defaultMessage: "Scene" })}
      loading={loading && !scene}
    >
      {scene && <SceneEditForm scene={scene} onSaved={onClose} />}
    </EntityEditSheet>
  );
}
